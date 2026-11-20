package inbox

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const maxBodyBytes = 1 << 20

// Handler serves the webhook receiver plus health and stats endpoints.
func (i *Inbox) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", i.receive)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /inbox/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(i.Stats())
	})
	return mux
}

func (i *Inbox) receive(w http.ResponseWriter, r *http.Request) {
	if i.opts.Token != "" {
		given := r.Header.Get(i.opts.TokenHeader)
		if subtle.ConstantTimeCompare([]byte(given), []byte(i.opts.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	ev, err := Parse(time.Now(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	i.Record(ev)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
