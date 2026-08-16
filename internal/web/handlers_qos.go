package web

import (
	"net/http"

	"frpmon/internal/qos"
)

func (s *Server) handleQosStatus(w http.ResponseWriter, r *http.Request) {
	if s.qos == nil {
		writeJSON(w, http.StatusOK, qos.Status{Mode: "off"})
		return
	}
	writeJSON(w, http.StatusOK, s.qos.Status())
}
