package infrastructure

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// responseReadError separates remote body failures from local artifact writes.
type responseReadError struct{ error }

func (e responseReadError) Unwrap() error { return e.error }

type responseReader struct{ io.Reader }

func (r responseReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = responseReadError{err}
	}
	return n, err
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryableNetwork(err error) bool {
	var cert *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var dns *net.DNSError
	if errors.As(err, &cert) || errors.As(err, &unknown) || errors.As(err, &invalid) || errors.As(err, &hostname) || (errors.As(err, &dns) && dns.IsNotFound) {
		return false
	}
	var body responseReadError
	var network net.Error
	return errors.As(err, &body) || errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF)
}

func retryStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryDelay(resp *http.Response, attempt int, minimum time.Duration) (time.Duration, error) {
	delay := time.Duration(2<<attempt) * time.Second
	if resp != nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) {
		value := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
			if seconds > 30 {
				return 0, fmt.Errorf("Retry-After exceeds 30s: %s", value)
			}
			delay = time.Duration(seconds) * time.Second
		} else if date, err := http.ParseTime(value); err == nil {
			delay = max(0, time.Until(date))
			if delay > 30*time.Second {
				return 0, fmt.Errorf("Retry-After exceeds 30s: %s", value)
			}
		}
	}
	return max(delay, minimum), nil
}

// getWithRetry consumes each response inside its attempt, before publishing any
// content or ETag. Only GET transport/body failures and selected statuses retry.
func getWithRetry(client *http.Client, req *http.Request, minimum time.Duration, consume func(*http.Response) error) error {
	for attempt := range 3 {
		if err := req.Context().Err(); err != nil {
			return err
		}
		resp, err := getAttempt(client, req, consume) //nolint:bodyclose // getAttempt closes every response before returning headers/status.
		if req.Context().Err() != nil {
			return req.Context().Err()
		}
		if err == nil || attempt == 2 || !retryableResponse(resp, err) {
			return err
		}
		delay, delayErr := retryDelay(resp, attempt, minimum)
		if delayErr != nil {
			return delayErr
		}
		if err := waitContext(req.Context(), delay); err != nil {
			return err
		}
	}
	return nil
}

func retryableResponse(resp *http.Response, err error) bool {
	return (resp != nil && retryStatus(resp.StatusCode)) || retryableNetwork(err)
}

func getAttempt(client *http.Client, req *http.Request, consume func(*http.Response) error) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // Body reads report remote failures.
	if retryStatus(resp.StatusCode) {
		return resp, fmt.Errorf("HTTP %s", resp.Status)
	}
	return resp, consume(resp)
}
