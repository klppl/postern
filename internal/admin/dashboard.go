package admin

import (
	"net/http"
	"time"

	"github.com/alexander/postern/internal/store"
)

type dashboardData struct {
	Counts       store.StatusCounts
	Recent       []*store.OutboxMessage
	Volume       []store.HourlyVolume
	MaxVolume    int
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-24 * time.Hour)
	counts, err := s.store.StatusCountsSince(r.Context(), since)
	if err != nil {
		s.log.Error("counts", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	recent, _, err := s.store.ListMessages(r.Context(), store.MessageFilter{Limit: 20})
	if err != nil {
		s.log.Error("recent", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	vol, err := s.store.VolumeByHour(r.Context(), since)
	if err != nil {
		s.log.Error("volume", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	maxV := 0
	for _, v := range vol {
		t := v.Sent + v.Failed
		if t > maxV {
			maxV = t
		}
	}
	s.render(w, r, "dashboard.html", "Dashboard", "dashboard", dashboardData{
		Counts: counts, Recent: recent, Volume: vol, MaxVolume: maxV,
	})
}
