package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortieAPI(t *testing.T) {
	httpRequest := func(method, url string, body io.Reader) *http.Request {
		request, err := http.NewRequest(method, url, body)
		if err != nil {
			t.Error(err)
		}
		return request
	}

	tests := []struct {
		name           string
		setup          func(t *testing.T, storage urlStorage)
		httpRequest    *http.Request
		expectedStatus int
		expectedBody   string
		expectations   func(t *testing.T, storage urlStorage)
	}{
		{
			name:           "create a url",
			httpRequest:    httpRequest(http.MethodPost, "/shortie", bytes.NewReader([]byte(`{"url":"https://example.com/data/hi"}`))),
			expectedStatus: http.StatusOK,
			expectedBody:   `{"shortUrl": "http://localhost:8421/shortie/4e24c46962"}`,
		},
		{
			name: "create a existing url",
			setup: func(t *testing.T, storage urlStorage) {
				err := storage.SaveURL(context.Background(), "4e24c46962", "https://example.com/data/hi", 0)
				require.NoError(t, err)
				_, _ = storage.GetURL(context.Background(), "4e24c46962")
			},
			httpRequest:    httpRequest(http.MethodPost, "/shortie", bytes.NewReader([]byte(`{"url":"https://example.com/data/hi"}`))),
			expectedStatus: http.StatusOK,
			expectedBody:   `{"shortUrl": "http://localhost:8421/shortie/4e24c46962"}`,
			expectations: func(t *testing.T, storage urlStorage) {
				usage, err := storage.GetStatistics(context.Background(), "4e24c46962")
				require.NoError(t, err)
				assert.True(t, len(usage) > 0)
			},
		},
		{
			name: "get /shortie/111 redirect",
			setup: func(t *testing.T, storage urlStorage) {
				err := storage.SaveURL(context.Background(), "111", "http://redirection.com/portal/portal", 0)
				require.NoError(t, err)
			},
			httpRequest:    httpRequest(http.MethodGet, "/shortie/111", nil),
			expectedStatus: http.StatusTemporaryRedirect,
		},
		{
			name: "get /shortie/222 not found",
			setup: func(t *testing.T, storage urlStorage) {
				err := storage.SaveURL(context.Background(), "111", "http://redirection.com/portal/portal", 0)
				require.NoError(t, err)
			},
			httpRequest:    httpRequest(http.MethodGet, "/shortie/222", nil),
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "delete /shortie/111",
			setup: func(t *testing.T, storage urlStorage) {
				err := storage.SaveURL(context.Background(), "111", "http://redirection.com/portal/portal", 0)
				require.NoError(t, err)
			},
			httpRequest:    httpRequest(http.MethodDelete, "/shortie/111", nil),
			expectedStatus: http.StatusOK,
		},
		{
			name: "delete /shortie/222 idempotent",
			setup: func(t *testing.T, storage urlStorage) {
				err := storage.SaveURL(context.Background(), "111", "http://redirection.com/portal/portal", 0)
				require.NoError(t, err)
			},
			httpRequest:    httpRequest(http.MethodDelete, "/shortie/222", nil),
			expectedStatus: http.StatusOK,
		},
		{
			name: "get usage - empty",
			setup: func(t *testing.T, storage urlStorage) {
				err := storage.SaveURL(context.Background(), "111", "http://redirection.com/portal/portal", 0)
				require.NoError(t, err)
				_, _ = storage.GetURL(context.Background(), "111")
			},
			httpRequest:    httpRequest(http.MethodGet, "/shortie/222/stats", nil),
			expectedStatus: http.StatusOK,
			expectedBody:   `{"lastDay":0,"lastWeek":0,"allTime":0}`,
		},
		{
			name: "get usage",
			setup: func(t *testing.T, storage urlStorage) {
				err := storage.SaveURL(context.Background(), "111", "http://redirection.com/portal/portal", 0)
				require.NoError(t, err)
				_, _ = storage.GetURL(context.Background(), "111")
				_, _ = storage.GetURL(context.Background(), "111")
				_, _ = storage.GetURL(context.Background(), "111")
			},
			httpRequest:    httpRequest(http.MethodGet, "/shortie/111/stats", nil),
			expectedStatus: http.StatusOK,
			expectedBody:   `{"lastDay":3,"lastWeek":3,"allTime":3}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := &LocalStorage{
				Objects: map[string]URLObject{},
				lock:    sync.Mutex{},
			}
			if test.setup != nil {
				test.setup(t, storage)
			}

			router := shortieAPI{storage: storage}.GetRouter()
			w := httptest.NewRecorder()
			router.ServeHTTP(w, test.httpRequest)
			assert.Equal(t, test.expectedStatus, w.Code)
			if test.expectedBody != "" {
				assert.JSONEq(t, test.expectedBody, w.Body.String())
			}
			if test.expectations != nil {
				test.expectations(t, storage)
			}
		})
	}
}
