package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/haraqa/haraqa/internal/headers"
)

// HandleOptions handles requests to the /topics/... endpoints with method == OPTIONS
func (s *Server) HandleOptions(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		_ = r.Body.Close()
	}
	requestHeaders := r.Header.Values("Access-Control-Request-Headers")
	allowedHeaders := make([]string, 0, len(requestHeaders))
	for _, v := range requestHeaders {
		canonicalHeader := http.CanonicalHeaderKey(strings.TrimSpace(v))
		if canonicalHeader == "" {
			continue
		}
		allowedHeaders = append(allowedHeaders, canonicalHeader)
	}
	if len(allowedHeaders) > 0 {
		w.Header()["Access-Control-Allow-Headers"] = allowedHeaders
	}
	method := r.Header.Get("Access-Control-Request-Method")
	w.Header().Set("Access-Control-Allow-Methods", method)
	w.WriteHeader(http.StatusOK)
}

// HandleGetAllTopics handles requests to the /topics endpoints with method == GET.
// It returns all topics currently defined in the queue as either a json or csv depending on the
// request content-type header
func (s *Server) HandleGetAllTopics(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	topics, err := s.q.ListTopics(query.Get("prefix"), query.Get("suffix"), query.Get("regex"))
	if err != nil {
		headers.SetError(w, err)
		return
	}
	if topics == nil {
		topics = []string{}
	}

	var response []byte
	switch r.Header.Get("Accept") {
	case "application/json":
		w.Header()[headers.ContentType] = []string{"application/json"}
		response, _ = json.Marshal(map[string][]string{
			"topics": topics,
		})
	default:
		w.Header()[headers.ContentType] = []string{"text/csv"}
		response = []byte(strings.Join(topics, ","))
	}
	_, _ = w.Write(response)
}

// HandleCreateTopic handles requests to the /topics/... endpoints with method == PUT.
// It will create a topic if the topic does not exist.
func (s *Server) HandleCreateTopic(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		_ = r.Body.Close()
	}

	topic, err := getTopic(r)
	if err != nil {
		headers.SetError(w, err)
		return
	}
	err = s.q.CreateTopic(topic)
	if err != nil {
		headers.SetError(w, err)
		return
	}
	w.Header()[headers.ContentType] = []string{"text/plain"}
	w.WriteHeader(http.StatusCreated)
}

// HandleModifyTopic handles requests to the /topics/... endpoints with method == PATCH.
// It will modify the topic if the topic exists. This is used to truncate topics by message
// offset or mod time.
func (s *Server) HandleModifyTopic(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		headers.SetError(w, headers.ErrInvalidBodyMissing)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	topic, err := getTopic(r)
	if err != nil {
		headers.SetError(w, err)
		return
	}

	var request headers.ModifyRequest
	if err = json.NewDecoder(r.Body).Decode(&request); err != nil {
		headers.SetError(w, headers.ErrInvalidBodyJSON)
		return
	}

	if request.Truncate == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	info, err := s.q.ModifyTopic(topic, request)
	if err != nil {
		headers.SetError(w, err)
		return
	}
	w.Header()[headers.ContentType] = []string{"application/json"}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(&info)
}

// HandleDeleteTopic handles requests to the /topics/... endpoints with method == DELETE.
// It will delete a topic if the topic exists.
func (s *Server) HandleDeleteTopic(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		_ = r.Body.Close()
	}

	topic, err := getTopic(r)
	if err != nil {
		headers.SetError(w, err)
		return
	}
	err = s.q.DeleteTopic(topic)
	if err != nil {
		headers.SetError(w, err)
		return
	}
	w.Header()[headers.ContentType] = []string{"text/plain"}
	w.WriteHeader(http.StatusNoContent)
}

// HandleProduce handles requests to the /topics/... endpoints with method == POST.
// It will add the given messages to the queue topic
func (s *Server) HandleProduce(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		headers.SetError(w, headers.ErrInvalidBodyMissing)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	topic, err := getTopic(r)
	if err != nil {
		headers.SetError(w, err)
		return
	}

	sizes, err := headers.ReadSizes(r.Header)
	if err != nil {
		headers.SetError(w, err)
		return
	}

	err = s.q.Produce(topic, sizes, uint64(time.Now().Unix()), r.Body)
	if err != nil {
		headers.SetError(w, err)
		return
	}
	s.metrics.ProduceMsgs(len(sizes))
	w.Header()[headers.ContentType] = []string{"text/plain"}
	w.WriteHeader(http.StatusNoContent)
}

// HandleConsume handles requests to the /topics/... endpoints with method == GET.
// It will retrieve messages from the queue topic
func (s *Server) HandleConsume(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		_ = r.Body.Close()
	}

	topic, err := getTopic(r)
	if err != nil {
		headers.SetError(w, err)
		return
	}

	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		headers.SetError(w, headers.ErrInvalidMessageID)
		return
	}

	limit := s.defaultConsumeLimit
	queryLimit := r.URL.Query().Get("limit")
	if queryLimit != "" && queryLimit[0] != '-' {
		limit, err = strconv.ParseInt(queryLimit, 10, 64)
		if err != nil {
			headers.SetError(w, headers.ErrInvalidMessageLimit)
			return
		}
		if limit <= 0 {
			limit = s.defaultConsumeLimit
		}
	}

	count, err := s.q.Consume(topic, id, limit, w)
	if err != nil {
		headers.SetError(w, err)
		return
	}
	if count == 0 {
		headers.SetError(w, headers.ErrNoContent)
		return
	}
	s.metrics.ConsumeMsgs(count)
}

func getTopic(r *http.Request) (string, error) {
	topic := filepath.Clean(strings.ToLower(strings.TrimPrefix(r.URL.Path, "/topics/")))
	if topic == "" || topic == "." {
		return "", headers.ErrInvalidTopic
	}
	return topic, nil
}
