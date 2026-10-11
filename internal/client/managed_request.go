package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
)

// maxErrorMessageLength bounds server messages copied into diagnostics.
const maxErrorMessageLength = 1000

// mayHaveCompleted warns that a failed write's outcome is unknown.
const mayHaveCompleted = "The change may have been applied; refresh, and import the resource if it exists, before retrying"

// sentError marks a failure once a request was attempted, when the server may
// have acted on it (even a refused connection can follow a redirect from a
// write that was applied); failures before sending, such as fetching a token,
// are returned as they are.
type sentError struct{ err error }

func (e *sentError) Error() string { return e.err.Error() }
func (e *sentError) Unwrap() error { return e.err }

// sender sends the generated client's requests, marking transport failures.
type sender struct{ client *http.Client }

func (s sender) Do(req *http.Request) (*http.Response, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &sentError{err}
	}
	return resp, nil
}

// newAPI returns the generated API client, authenticated with this client's token.
func (c *Client) newAPI() (*generated.Client, error) {
	return generated.NewClient(c.host+"/api", generated.WithHTTPClient(sender{c.httpClient}), generated.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
		token, err := c.getToken(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}))
}

// call performs one generated API call and decodes its response into result.
// Reads are retried; a write is sent once, because replaying a mutation with an
// ambiguous outcome can race or duplicate it (a dataset delete, for one, can
// outlast the client timeout). Error messages keep the server's explanation
// with every submitted secret redacted.
func (c *Client) call(read bool, send func(*generated.Client) (*http.Response, error), result any, secrets ...string) error {
	api, err := c.newAPI()
	if err != nil {
		return err
	}
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		resp, err = send(api)
		var sent *sentError
		retry := errors.As(err, &sent) || (err == nil && resp.StatusCode >= 500)
		if !read || !retry || attempt == maxRetries {
			break
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(calculateBackoff(attempt))
	}
	if err != nil {
		message := redact(err.Error(), secrets)
		var sent *sentError
		if read || !errors.As(err, &sent) {
			return errors.New(message)
		}
		return fmt.Errorf("%s. %s", message, mayHaveCompleted)
	}
	var raw json.RawMessage
	if err := handleResponse(resp, &raw); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			message := errorMessage(apiErr, secrets)
			// A server error can follow a committed write, e.g. a create that saved
			// the integration and then failed to provision its topics.
			if !read && apiErr.StatusCode >= 500 {
				message += ". " + mayHaveCompleted
			}
			return &APIError{StatusCode: apiErr.StatusCode, Message: message}
		}
		message := fmt.Sprintf("%s %s returned an unreadable response: %s", resp.Request.Method, resp.Request.URL.Path, redact(err.Error(), secrets))
		if !read {
			message += ". " + mayHaveCompleted
		}
		return errors.New(message)
	}
	if result == nil || len(raw) == 0 {
		return nil
	}
	// Numbers stay json.Number so large integers in payloads are not rounded.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("%s %s returned an unexpected response: %w", resp.Request.Method, resp.Request.URL.Path, err)
	}
	return nil
}

func errorMessage(apiErr *APIError, secrets []string) string {
	message := strings.TrimSpace(redact(apiErr.Message, secrets))
	if message == "" {
		return http.StatusText(apiErr.StatusCode)
	}
	// Cut by character so the diagnostic stays valid UTF-8.
	if runes := []rune(message); len(runes) > maxErrorMessageLength {
		message = string(runes[:maxErrorMessageLength]) + "..."
	}
	return message
}

// redact removes each secret in its raw and JSON-escaped forms, since servers
// may echo submitted values either way.
func redact(message string, secrets []string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		message = strings.ReplaceAll(message, secret, "[redacted]")
		var escaped bytes.Buffer
		encoder := json.NewEncoder(&escaped)
		encoder.SetEscapeHTML(false)
		if encoder.Encode(secret) == nil {
			quoted := strings.TrimSpace(escaped.String())
			message = strings.ReplaceAll(message, quoted[1:len(quoted)-1], "[redacted]")
		}
	}
	return message
}

// IsNotFound reports whether err is an API 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.IsNotFound()
}
