package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/alexander/postern/internal/store"
	"github.com/go-chi/chi/v5"
)

type messageListData struct {
	Messages   []*store.OutboxMessage
	Total      int
	Limit      int
	Offset     int
	NextOffset int
	PrevOffset int
	Filter     store.MessageFilter
	Keys       []*store.APIKey
	Statuses   []string
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.MessageFilter{
		Status:    q.Get("status"),
		Recipient: q.Get("recipient"),
		Limit:     100,
	}
	if v := q.Get("api_key_id"); v != "" {
		f.APIKeyID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.Since = t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.Until = t.Add(24 * time.Hour)
		}
	}
	if v := q.Get("offset"); v != "" {
		f.Offset, _ = strconv.Atoi(v)
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	msgs, total, err := s.store.ListMessages(r.Context(), f)
	if err != nil {
		s.log.Error("list messages", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	keys, _ := s.store.ListAPIKeys(r.Context())

	next := f.Offset + f.Limit
	if next >= total {
		next = -1
	}
	prev := f.Offset - f.Limit
	if prev < 0 {
		prev = -1
	}

	s.render(w, r, "messages.html", "Messages", "messages", messageListData{
		Messages:   msgs,
		Total:      total,
		Limit:      f.Limit,
		Offset:     f.Offset,
		NextOffset: next,
		PrevOffset: prev,
		Filter:     f,
		Keys:       keys,
		Statuses:   []string{"pending", "sending", "sent", "failed", "dead"},
	})
}

type messageDetailData struct {
	Message  *store.OutboxMessage
	Attempts []*store.OutboxAttempt
	APIKey   *store.APIKey
}

func (s *Server) messageDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m, err := s.store.GetMessage(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	attempts, _ := s.store.ListAttempts(r.Context(), id)
	key, _ := s.store.GetAPIKey(r.Context(), m.APIKeyID)
	s.render(w, r, "message_detail.html", "Message detail", "messages", messageDetailData{
		Message:  m,
		Attempts: attempts,
		APIKey:   key,
	})
}
