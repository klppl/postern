// Postern — minimal client-side glue.
// The only interactive feature is the template-preview form; everything
// else is server-rendered HTML over POST/GET.

(function () {
    "use strict";

    function setupPreview() {
        var form = document.querySelector('form[data-preview]');
        if (!form) return;
        var resultEl = document.getElementById(form.dataset.preview);
        if (!resultEl) return;
        form.addEventListener("submit", function (ev) {
            ev.preventDefault();
            var fd = new FormData();
            // Pull live values from the editor inputs above.
            var subject = document.querySelector('[name="subject"]');
            var bodyText = document.getElementById("body_text");
            var bodyHTML = document.getElementById("body_html");
            var vars = form.querySelector('[name="variables"]');
            if (subject) fd.set("subject", subject.value);
            if (bodyText) fd.set("body_text", bodyText.value);
            if (bodyHTML) fd.set("body_html", bodyHTML.value);
            if (vars) fd.set("variables", vars.value);
            fetch("/admin/templates/preview", {
                method: "POST",
                body: new URLSearchParams(fd),
                headers: { "Content-Type": "application/x-www-form-urlencoded" },
                credentials: "same-origin",
            }).then(function (r) {
                return r.text();
            }).then(function (html) {
                resultEl.innerHTML = html;
            });
        });
    }

    document.addEventListener("DOMContentLoaded", setupPreview);
})();
