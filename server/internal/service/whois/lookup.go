package whois

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	whoisclient "github.com/likexian/whois"
	whoisparser "github.com/likexian/whois-parser"
)

type Info struct {
	Domain        string     `json:"domain"`
	Registrar     string     `json:"registrar"`
	WhoisServer   string     `json:"whoisServer"`
	Status        []string   `json:"status"`
	NameServers   []string   `json:"nameServers"`
	CreatedAt     *time.Time `json:"createdAt"`
	UpdatedAt     *time.Time `json:"updatedAt"`
	ExpiresAt     *time.Time `json:"expiresAt"`
	Registrant    string     `json:"registrant"`
	RegistrantOrg string     `json:"registrantOrg"`
	Raw           string     `json:"raw"`
}

// LookupExpiresAt returns the domain expiration time parsed from WHOIS data.
func LookupExpiresAt(ctx context.Context, domain string) (*time.Time, error) {
	type result struct {
		expiresAt *time.Time
		err       error
	}

	ch := make(chan result, 1)
	go func() {
		client := whoisclient.NewClient()
		client.SetTimeout(8 * time.Second)

		raw, err := client.Whois(domain)
		if err != nil {
			expiresAt, rdapErr := lookupRDAPExpiresAt(ctx, domain)
			if rdapErr != nil {
				ch <- result{err: fmt.Errorf("%w；RDAP查询失败：%w", err, rdapErr)}
				return
			}
			ch <- result{expiresAt: expiresAt}
			return
		}

		info, err := whoisparser.Parse(raw)
		if err != nil {
			expiresAt, rdapErr := lookupRDAPExpiresAt(ctx, domain)
			if rdapErr != nil {
				ch <- result{err: fmt.Errorf("%w；RDAP查询失败：%w", err, rdapErr)}
				return
			}
			ch <- result{expiresAt: expiresAt}
			return
		}

		if info.Domain.ExpirationDateInTime == nil {
			expiresAt, err := lookupRDAPExpiresAt(ctx, domain)
			ch <- result{expiresAt: expiresAt, err: err}
			return
		}
		expiresAt := info.Domain.ExpirationDateInTime.UTC()
		ch <- result{expiresAt: &expiresAt}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.expiresAt, res.err
	}
}

func Lookup(ctx context.Context, domain string) (*Info, error) {
	type result struct {
		info *Info
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		client := whoisclient.NewClient()
		client.SetTimeout(10 * time.Second)

		raw, err := client.Whois(domain)
		if err != nil {
			ch <- result{err: err}
			return
		}
		info := &Info{Domain: domain, Raw: strings.TrimSpace(raw)}
		parsed, err := whoisparser.Parse(raw)
		if err == nil {
			fillInfoFromParsed(info, parsed)
		}
		if info.ExpiresAt == nil {
			if expiresAt, rdapErr := lookupRDAPExpiresAt(ctx, domain); rdapErr == nil {
				info.ExpiresAt = expiresAt
			}
		}
		ch <- result{info: info}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.info, res.err
	}
}

func fillInfoFromParsed(info *Info, parsed whoisparser.WhoisInfo) {
	if parsed.Domain != nil {
		info.Domain = firstNonEmpty(parsed.Domain.Domain, parsed.Domain.Name, info.Domain)
		info.WhoisServer = parsed.Domain.WhoisServer
		info.Status = parsed.Domain.Status
		info.NameServers = parsed.Domain.NameServers
		info.CreatedAt = utcTime(parsed.Domain.CreatedDateInTime)
		info.UpdatedAt = utcTime(parsed.Domain.UpdatedDateInTime)
		info.ExpiresAt = utcTime(parsed.Domain.ExpirationDateInTime)
	}
	if parsed.Registrar != nil {
		info.Registrar = firstNonEmpty(parsed.Registrar.Name, parsed.Registrar.Organization)
	}
	if parsed.Registrant != nil {
		info.Registrant = parsed.Registrant.Name
		info.RegistrantOrg = parsed.Registrant.Organization
	}
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func lookupRDAPExpiresAt(ctx context.Context, domain string) (*time.Time, error) {
	type rdapEvent struct {
		EventAction string `json:"eventAction"`
		EventDate   string `json:"eventDate"`
	}
	type rdapResponse struct {
		Events []rdapEvent `json:"events"`
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://rdap.org/domain/"+url.PathEscape(domain), nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP状态码：%d", resp.StatusCode)
	}

	var body rdapResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	for _, event := range body.Events {
		if event.EventAction != "expiration" && event.EventAction != "registration expiration" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, event.EventDate)
		if err != nil {
			return nil, err
		}
		expiresAt := parsed.UTC()
		return &expiresAt, nil
	}
	return nil, nil
}
