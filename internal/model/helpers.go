package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/derailed/tview"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetaFQN returns a fully qualified resource name.
func MetaFQN(m metav1.ObjectMeta) string {
	return FQN(m.Namespace, m.Name)
}

// FQN returns a fully qualified resource name.
func FQN(ns, n string) string {
	if ns == "" {
		return n
	}
	return ns + "/" + n
}

// Truncate a string to the given l and suffix ellipsis if needed.
func Truncate(str string, width int) string {
	return runewidth.Truncate(str, width, string(tview.SemigraphicsHorizontalEllipsis))
}

// NewExpBackOff returns a new exponential backoff timer.
func NewExpBackOff(ctx context.Context, start, max time.Duration) backoff.BackOffContext {
	bf := backoff.NewExponentialBackOff()
	bf.InitialInterval, bf.MaxElapsedTime = start, max
	return backoff.WithContext(bf, ctx)
}

func FetchLatestRev(userName string, repoName string) (string, error) {
	log.Info().Msgf("Fetching latest %s rev...", repoName)
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", userName, repoName)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	m := make(map[string]interface{}, 20)
	if err := json.Unmarshal(b, &m); err != nil {
		return "", err
	}

	if v, ok := m["name"]; ok {
		log.Info().Msgf("%s latest rev: %q", repoName, v.(string))
		return v.(string), nil
	}

	return "", errors.New("No version found")
}
