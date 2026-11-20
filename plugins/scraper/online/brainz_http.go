package online

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

// MusicBrainz 和 AcoustID 按整个应用限速，多个实例共享同一服务的发起时间。
type requestPacer struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func (p *requestPacer) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.mu.Lock()
		wait := time.Until(p.next)
		if wait <= 0 {
			p.next = time.Now().Add(p.interval)
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

var (
	brainzPacer       = requestPacer{interval: time.Second}
	acoustPacer       = requestPacer{interval: time.Second/3 + time.Millisecond}
	errCatalogMissing = errors.New("catalog entry not found")
)

const catalogUserAgent = "fnMrRice/MusicFS/0.1 (https://github.com/fnMrRice/MusicFS)"

type cooldown struct {
	until  time.Time
	status int
}

// 同一服务返回退避提示后，所有实例在规定时间内停止向该主机发送请求。
var catalogCooldown = struct {
	sync.Mutex
	byHost map[string]cooldown
}{byHost: make(map[string]cooldown)}

func retryDeadline(value string, now time.Time) time.Time {
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	deadline, _ := http.ParseTime(value)
	return deadline
}

func catalogRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	catalogCooldown.Lock()
	prior := catalogCooldown.byHost[host]
	catalogCooldown.Unlock()
	if remaining := time.Until(prior.until); remaining > 0 {
		seconds := int((remaining-1)/time.Second) + 1
		return nil, &fault.Error{Code: "RATE_LIMITED", Message: "Catalog lookup is waiting for the upstream cooldown", RetryAfter: &seconds,
			Scraper: &fault.ScraperFailure{Reason: "cooldown", UpstreamStatus: prior.status}}
	}
	if client == nil {
		client = http.DefaultClient
	}
	req.Header.Set("User-Agent", catalogUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fault.Fail("SOURCE_UNAVAILABLE", "catalog service request failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, errCatalogMissing
		}
		failure := &fault.Error{Code: "SOURCE_UNAVAILABLE", Message: fmt.Sprintf("Catalog service returned HTTP %d", resp.StatusCode),
			Scraper: &fault.ScraperFailure{Reason: "upstream", UpstreamStatus: resp.StatusCode}}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			now := time.Now()
			if deadline := retryDeadline(resp.Header.Get("Retry-After"), now); !deadline.IsZero() {
				seconds := 0
				failure.RetryAfter = &seconds
				if remaining := deadline.Sub(now); remaining > 0 {
					seconds = int((remaining-1)/time.Second) + 1
					catalogCooldown.Lock()
					if deadline.After(catalogCooldown.byHost[host].until) {
						catalogCooldown.byHost[host] = cooldown{until: deadline, status: resp.StatusCode}
					}
					catalogCooldown.Unlock()
				}
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				failure.Code = "RATE_LIMITED"
			}
		}
		return nil, failure
	}
	return resp, nil
}

func readCatalogJSON(client *http.Client, req *http.Request, target any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := catalogRequest(client, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	const maxResponse = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponse {
		return errors.New("catalog service response is too large")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("invalid catalog service response")
	}
	return nil
}
