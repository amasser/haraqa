package server

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/haraqa/haraqa/internal/headers"
	"github.com/pkg/errors"
)

func TestServer_HandleGetAllTopics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	topics := []string{"topic_1", "topic_2", "topic_3"}
	q := NewMockQueue(ctrl)
	gomock.InOrder(
		q.EXPECT().RootDir().Times(1).Return(""),
		q.EXPECT().ListTopics("", "", "").Return(topics, nil).Times(2),
		q.EXPECT().ListTopics("p", "s", "r").Return(nil, nil).Times(1),
		q.EXPECT().ListTopics("", "", "").Return(nil, errors.New("test get topics error")).Times(1),
		q.EXPECT().Close().Return(nil).Times(1),
	)
	s, err := NewServer(WithQueue(q))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// valid request, happy path, csv output
	{
		w := httptest.NewRecorder()
		r, err := http.NewRequest(http.MethodGet, "/topics", bytes.NewBuffer([]byte("test body")))
		if err != nil {
			t.Fatal(err)
		}
		s.ServeHTTP(w, r)
		resp := w.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatal(resp.Status)
		}
		err = headers.ReadErrors(resp.Header)
		if err != nil {
			t.Fatal(err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != strings.Join(topics, ",") {
			t.Fatal(string(body))
		}
	}

	// valid request, happy path, json output
	{
		w := httptest.NewRecorder()
		r, err := http.NewRequest(http.MethodGet, "/topics", bytes.NewBuffer([]byte("test body")))
		if err != nil {
			t.Fatal(err)
		}
		r.Header["Accept"] = []string{"application/json"}
		s.ServeHTTP(w, r)
		resp := w.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatal(resp.Status)
		}
		err = headers.ReadErrors(resp.Header)
		if err != nil {
			t.Fatal(err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		j, err := json.Marshal(struct {
			Topics []string `json:"topics"`
		}{Topics: topics})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, j) {
			t.Fatal(string(body))
		}
	}

	// valid request, nil path, json output
	{
		w := httptest.NewRecorder()
		r, err := http.NewRequest(http.MethodGet, "/topics?prefix=p&suffix=s&regex=r", bytes.NewBuffer([]byte("test body")))
		if err != nil {
			t.Fatal(err)
		}
		r.Header["Accept"] = []string{"application/json"}
		s.ServeHTTP(w, r)
		resp := w.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatal(resp.Status)
		}
		err = headers.ReadErrors(resp.Header)
		if err != nil {
			t.Fatal(err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		j, err := json.Marshal(struct {
			Topics []string `json:"topics"`
		}{Topics: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, j) {
			t.Fatal(string(body))
		}
	}

	// valid request, queue error: unknown error
	{
		w := httptest.NewRecorder()
		r, err := http.NewRequest(http.MethodGet, "/topics", bytes.NewBuffer([]byte("test body")))
		if err != nil {
			t.Fatal(err)
		}
		s.ServeHTTP(w, r)
		resp := w.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatal(resp.Status)
		}
		err = headers.ReadErrors(resp.Header)
		if err.Error() != "test get topics error" {
			t.Fatal(err)
		}
	}
}
