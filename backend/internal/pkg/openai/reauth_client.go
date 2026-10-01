package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

// reauthIssuer is the OpenAI authentication issuer used by the password login
// flow. It must match oauth.go's AuthorizeURL host.
const reauthIssuer = "https://auth.openai.com"

// sentinelEndpoint is OpenAI's proof-of-work challenge endpoint. The client
// performs the same PoW the browser SDK would, then sends the challenge token
// back to the auth APIs.
const sentinelEndpoint = "https://sentinel.openai.com/backend-api/sentinel/req"

const (
	reauthFlowAuthorizeContinue = "authorize_continue"
	reauthFlowPasswordVerify    = "password_verify"
	reauthMaxHops               = 16
	reauthMaxPoWAttempts        = 500000
)

// reauthUserAgent mirrors a desktop Chrome fingerprint.
const reauthUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.6943.153 Safari/537.36"

// reauthErrorPrefix matches the SDK's error-blob prefix for failed PoW.
const reauthErrorPrefix = "wQ8Lk5FbGpA2NcR9dShT6gYjU7VxZ4D"

type reauthHTTPError struct {
	StatusCode int
	Body       string
}

func (e *reauthHTTPError) Error() string {
	body := e.Body
	if len(body) > 300 {
		body = body[:300]
	}
	return fmt.Sprintf("openai reauth http %d: %s", e.StatusCode, body)
}

// apiError extracts a stable machine-readable code when the upstream returns
// one, otherwise falls back to a generic code.
func apiError(body string, fallback string) string {
	if body == "" {
		return fallback
	}
	var parsed struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
		Code string `json:"code"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil {
		if parsed.Error.Code != "" {
			return parsed.Error.Code
		}
		if parsed.Error.Type != "" {
			return parsed.Error.Type
		}
		if parsed.Code != "" {
			return parsed.Code
		}
		if parsed.Type != "" {
			return parsed.Type
		}
	}
	return fallback
}

// openAISentinel implements the FNV-1a based proof-of-work used by
// sentinel.openai.com. It is a reimplementation of the browser SDK's
// requirements token generator, producing client-side proof without
// bypassing the challenge.
type openAISentinel struct {
	deviceID string
	ua       string
}

// fnv1a32 reproduces the SDK's mixed FNV-1a hash (with murmur finalizer).
func fnv1a32(text string) string {
	h := uint32(2166136261)
	for _, ch := range text {
		h ^= uint32(ch)
		h *= 16777619
	}
	h ^= h >> 16
	h *= 2246822507
	h ^= h >> 13
	h *= 3266489909
	h ^= h >> 16
	return fmt.Sprintf("%08x", h)
}

func randFraction() float64 {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<53))
	if err != nil {
		n = big.NewInt(123456789)
	}
	return float64(n.Int64()) / float64(1<<53)
}

// config assembles the browser-environment array the SDK hashes. Values are
// representative of a real Chrome session; the server only verifies shape and
// the PoW relation between p and the challenge.
func (s *openAISentinel) config(now time.Time) []any {
	dateStr := now.UTC().Format("Mon Jan 02 2006 15:04:05") + " GMT+0000 (Coordinated Universal Time)"
	perfNow := 1000.0 + randFraction()*49000
	return []any{
		"1920x1080", dateStr, 4294705152, randFraction(), s.ua,
		"https://sentinel.openai.com/sentinel/20260124ceb8/sdk.js",
		nil, nil, "en-US", "en-US,en",
		randFraction(), "vendor\u2212undefined", "URL", "parseFloat", perfNow,
		uuid.NewString(), "", 8, float64(now.UnixMilli()) - perfNow,
	}
}

func base64JSON(v []any) string {
	b, _ := json.Marshal(v)
	return base64.StdEncoding.EncodeToString(b)
}

// requirementsToken produces the token the SDK sends before any challenge was
// issued (the "p" field of the initial request).
func (s *openAISentinel) requirementsToken() string {
	cfg := s.config(time.Now())
	cfg[3] = 1
	return "gAAAAAC" + base64JSON(cfg)
}

// runCheck performs a single PoW attempt.
func (s *openAISentinel) runCheck(start time.Time, seed, difficulty string, cfg []any, nonce int) string {
	cfg[3] = nonce
	cfg[9] = float64(time.Since(start).Milliseconds())
	data := base64JSON(cfg)
	hash := fnv1a32(seed + data)
	if sentinelDifficultySatisfied(hash, difficulty) {
		return data + "~S"
	}
	return ""
}

func sentinelDifficultySatisfied(hash, difficulty string) bool {
	difficulty = strings.ToLower(strings.TrimSpace(difficulty))
	if difficulty == "" {
		difficulty = "0"
	}
	if len(difficulty) > len(hash) {
		return false
	}
	for _, r := range difficulty {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return hash[:len(difficulty)] <= difficulty
}

func (s *openAISentinel) solvePoW(seed, difficulty string) string {
	start := time.Now()
	cfg := s.config(start)
	for i := 0; i < reauthMaxPoWAttempts; i++ {
		if result := s.runCheck(start, seed, difficulty, cfg, i); result != "" {
			return "gAAAAAB" + result
		}
	}
	return "gAAAAAB" + reauthErrorPrefix + base64JSON([]any{nil})
}

// pValue generates the proof-of-work value: solves the server challenge when
// one is required, otherwise emits a fresh requirements token.
func (s *openAISentinel) pValue(challenge map[string]any) string {
	pow, _ := challenge["proofofwork"].(map[string]any)
	if required, _ := pow["required"].(bool); required {
		if seed, _ := pow["seed"].(string); seed != "" {
			difficulty, _ := pow["difficulty"].(string)
			if difficulty == "" {
				difficulty = "0"
			}
			return s.solvePoW(seed, difficulty)
		}
	}
	return s.requirementsToken()
}

// tValue fabricates the long base64 environment blob the SDK attaches. The
// server only checks that the field is present and shaped like the SDK value.
func tValue() string {
	buf := make([]byte, 3500)
	_, _ = rand.Read(buf)
	enc := base64.StdEncoding.EncodeToString(buf)
	return "Th" + enc[2:]
}

// fetchChallenge requests the sentinel challenge for the given flow.
func (s *openAISentinel) fetchChallenge(ctx context.Context, transport http.RoundTripper, flow string) (map[string]any, error) {
	body := map[string]any{
		"p":    s.requirementsToken(),
		"id":   s.deviceID,
		"flow": flow,
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sentinelEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Referer", "https://sentinel.openai.com/backend-api/sentinel/frame.html")
	req.Header.Set("Origin", "https://sentinel.openai.com")
	req.Header.Set("User-Agent", s.ua)
	req.Header.Set("sec-ch-ua", `"Not(A:Brand";v="99", "Google Chrome";v="133", "Chromium";v="133"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sentinel challenge http %d", resp.StatusCode)
	}
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

// buildToken assembles the full openai-sentinel-token JSON string for the flow.
func (s *openAISentinel) buildToken(ctx context.Context, transport http.RoundTripper, flow string) (string, error) {
	challenge, err := s.fetchChallenge(ctx, transport, flow)
	if err != nil {
		return "", err
	}
	cValue, _ := challenge["token"].(string)
	if cValue == "" {
		return "", errors.New("sentinel challenge missing token")
	}
	token := map[string]any{
		"p":    s.pValue(challenge),
		"t":    tValue(),
		"c":    cValue,
		"id":   s.deviceID,
		"flow": flow,
	}
	b, _ := json.Marshal(token)
	return string(b), nil
}

// reauthClient is a self-contained browser-like session for the OAuth
// password login flow. It owns its own cookie jar and never shares state
// across calls: every LoginWithCredentialsForAccount invocation is isolated.
type reauthClient struct {
	deviceID      string
	ua            string
	workspaceHint string
	expectedState string
	http          *http.Client
}

func allowedOAuthRedirectHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "auth.openai.com" || strings.HasSuffix(host, ".auth.openai.com") ||
		host == "chatgpt.com" || strings.HasSuffix(host, ".chatgpt.com")
}

func configureReauthRedirects(client *http.Client) {
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if strings.EqualFold(next.URL.Hostname(), "localhost") || next.URL.Hostname() == "127.0.0.1" {
			return http.ErrUseLastResponse
		}
		if !allowedOAuthRedirectHost(next.URL.Hostname()) {
			return fmt.Errorf("refusing OAuth redirect to untrusted host")
		}
		return nil
	}
}

func newReauthClient(ctx context.Context, proxyURL, workspaceHint string) (*reauthClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 90 * time.Second,
	}
	if proxyURL != "" {
		proxyURLParsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy url: %w", err)
		}
		client.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURLParsed)}
	} else {
		client.Transport = http.DefaultTransport
	}
	configureReauthRedirects(client)
	return &reauthClient{deviceID: uuid.NewString(), ua: reauthUserAgent, workspaceHint: workspaceHint, http: client}, nil
}

// do performs a request through the shared session, applying the same headers
// a browser would. API calls never follow redirects (the caller inspects
// Location/code itself); page visits pass follow=true.
func (c *reauthClient) do(ctx context.Context, method, rawURL string, body any, headers http.Header, follow bool) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			reader = strings.NewReader(v)
		case []byte:
			reader = bytes.NewReader(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			reader = bytes.NewReader(b)
			if headers == nil {
				headers = http.Header{}
			}
			if headers.Get("Content-Type") == "" {
				headers.Set("Content-Type", "application/json")
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	if headers != nil {
		for k, vs := range headers {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.ua)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	// Datadog APM trace headers the auth frontend sends.
	req.Header.Set("traceparent", "00-0000000000000000"+randomHex(16)+"-"+randomHex(16)+"-01")
	req.Header.Set("tracestate", "dd=s:1;o:rum")
	req.Header.Set("x-datadog-origin", "rum")
	req.Header.Set("x-datadog-parent-id", randomInt63String())
	req.Header.Set("x-datadog-sampling-priority", "1")
	req.Header.Set("x-datadog-trace-id", randomInt63String())
	if follow {
		return c.http.Do(req)
	}
	// Disable auto-redirect so the caller can read Location and the final code.
	original := c.http.CheckRedirect
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer func() { c.http.CheckRedirect = original }()
	return c.http.Do(req)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%x", buf)
}

func randomInt63String() string {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 63))
	if err != nil {
		return "0"
	}
	return n.String()
}

// parseJSONResponse decodes a JSON body; the response is always consumed.
func parseJSONResponse(resp *http.Response) (map[string]any, error) {
	if resp == nil {
		return nil, errors.New("nil response")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		// Non-JSON (e.g. HTML) responses are still surfaced for error details.
		return map[string]any{"_raw": string(body)}, nil
	}
	return data, nil
}

// continueURLFromJSON matches the reference _extract_continue_url_from_json.
func continueURLFromJSON(data map[string]any) string {
	if raw, ok := data["continue_url"].(string); ok && raw != "" {
		return raw
	}
	if raw, ok := data["url"].(string); ok && raw != "" {
		return raw
	}
	if page, ok := data["page"].(map[string]any); ok {
		if payload, ok := page["payload"].(map[string]any); ok {
			if raw, ok := payload["url"].(string); ok {
				return raw
			}
		}
	}
	return ""
}

func pageType(data map[string]any) string {
	if page, ok := data["page"].(map[string]any); ok {
		if t, ok := page["type"].(string); ok {
			return t
		}
	}
	if t, ok := data["page_type"].(string); ok {
		return t
	}
	return ""
}

func normalizeContinueURL(next, current string) string {
	if next == "" {
		return ""
	}
	if strings.HasPrefix(next, "/") {
		return reauthIssuer + next
	}
	if strings.HasPrefix(next, "http") {
		return next
	}
	return current
}

// extractCodeFromURL pulls an authorization code out of a callback URL.
func extractCodeFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("code")
}

// visitPage performs a browser-like GET that advances the server-side auth
// state machine.
func (c *reauthClient) visitPage(ctx context.Context, target, referer string) error {
	if target == "" {
		return nil
	}
	headers := http.Header{}
	headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	headers.Set("Upgrade-Insecure-Requests", "1")
	if referer != "" {
		headers.Set("Referer", referer)
	}
	resp, err := c.do(ctx, http.MethodGet, target, nil, headers, true)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return nil
}

// doJSON posts JSON to an auth API with the sentinel token and device headers.
func (c *reauthClient) doJSON(ctx context.Context, method, rawURL string, payload map[string]any, referer, sentinel, flow string) (*http.Response, error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	headers.Set("Origin", reauthIssuer)
	headers.Set("oai-device-id", c.deviceID)
	if referer != "" {
		headers.Set("Referer", referer)
	}
	if sentinel != "" {
		headers.Set("openai-sentinel-token", sentinel)
	}
	if flow != "" {
		headers.Set("openai-flow", flow)
	}
	return c.do(ctx, method, rawURL, payload, headers, false)
}

// mfaFactors extracts TOTP factors from response data, recursively, matching
// the reference walker.
func mfaFactors(data map[string]any) []map[string]any {
	var found []map[string]any
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 8 || v == nil {
			return
		}
		switch val := v.(type) {
		case map[string]any:
			for _, key := range []string{"mfa_factors", "factors"} {
				if items, ok := val[key].([]any); ok {
					for _, item := range items {
						if m, ok := item.(map[string]any); ok {
							id, _ := m["id"].(string)
							factorType, _ := m["factor_type"].(string)
							if factorType == "" {
								factorType, _ = m["type"].(string)
							}
							if id != "" && factorType != "" {
								found = append(found, m)
							}
						}
					}
				}
			}
			for _, nested := range val {
				walk(nested, depth+1)
			}
		case []any:
			for _, nested := range val {
				walk(nested, depth+1)
			}
		}
	}
	walk(data, 0)

	// Deduplicate by (id, type).
	seen := map[string]bool{}
	var out []map[string]any
	for _, m := range found {
		id, _ := m["id"].(string)
		ft, _ := m["factor_type"].(string)
		if ft == "" {
			ft, _ = m["type"].(string)
		}
		key := id + "|" + strings.ToLower(ft)
		if seen[key] {
			continue
		}
		seen[key] = true
		m2 := make(map[string]any, len(m)+1)
		for k, v := range m {
			m2[k] = v
		}
		m2["factor_type"] = strings.ToLower(ft)
		out = append(out, m2)
	}
	return out
}

func findTOTPFactor(data map[string]any) map[string]any {
	for _, m := range mfaFactors(data) {
		if strings.EqualFold(m["factor_type"].(string), "totp") {
			return m
		}
	}
	return nil
}

// sessionData decodes the oai-client-auth-session cookie (plain JSON or base64
// with optional compression) into a map, mirroring the reference.
func (c *reauthClient) sessionData() map[string]any {
	jar, ok := c.http.Jar.(*cookiejar.Jar)
	if !ok {
		return nil
	}
	u, _ := url.Parse(reauthIssuer)
	for _, cookie := range jar.Cookies(u) {
		if cookie.Name != "oai-client-auth-session" {
			continue
		}
		for _, cand := range cookieCandidates(cookie.Value) {
			if data, ok := decodeCookieCandidate(cand); ok {
				return data
			}
		}
	}
	return nil
}

func cookieCandidates(raw string) []string {
	var out []string
	text := strings.TrimSpace(raw)
	if text == "" {
		return out
	}
	out = append(out, text)
	out = append(out, strings.Trim(text, `"'`))
	if strings.HasPrefix(text, "s:") {
		out = append(out, text[2:])
	}
	if unquoted, err := url.QueryUnescape(text); err == nil && unquoted != text {
		out = append(out, unquoted)
		out = append(out, strings.Trim(unquoted, `"'`))
		if strings.HasPrefix(unquoted, "s:") {
			out = append(out, unquoted[2:])
		}
	}
	return out
}

func decodeCookieCandidate(candidate string) (map[string]any, bool) {
	var data map[string]any
	if err := json.Unmarshal([]byte(candidate), &data); err == nil {
		if cas, ok := data["client_auth_session"].(map[string]any); ok {
			return cas, true
		}
		return data, true
	}
	padded := candidate + strings.Repeat("=", (4-len(candidate)%4)%4)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		blob, err := enc.DecodeString(padded)
		if err != nil {
			continue
		}
		if m, ok := decodeBlob(blob); ok {
			return m, true
		}
	}
	return nil, false
}

func decodeBlob(blob []byte) (map[string]any, bool) {
	var data map[string]any
	if err := json.Unmarshal(blob, &data); err == nil {
		if cas, ok := data["client_auth_session"].(map[string]any); ok {
			return cas, true
		}
		return data, true
	}
	return nil, false
}

// exchangeCodeForTokens performs the OAuth token exchange.
func (c *reauthClient) exchangeCodeForTokens(ctx context.Context, code, codeVerifier string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", DefaultRedirectURI)
	form.Set("client_id", ClientID)
	form.Set("code_verifier", codeVerifier)
	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	headers.Set("Accept", "application/json")
	resp, err := c.do(ctx, http.MethodPost, reauthIssuer+"/oauth/token", form.Encode(), headers, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &reauthHTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// selectWorkspace chooses the first workspace from the session cookie and
// returns an authorization code when the consent flow produces one directly.
func (c *reauthClient) selectWorkspace(ctx context.Context, consentURL string) (string, error) {
	data := c.sessionData()
	if data == nil {
		return "", errors.New("oauth session cookie could not be decoded")
	}
	workspaces, _ := data["workspaces"].([]any)
	if len(workspaces) == 0 {
		return "", errors.New("no workspace in oauth session")
	}
	workspaceID := ""
	if c.workspaceHint != "" {
		for _, item := range workspaces {
			workspace, _ := item.(map[string]any)
			if id, _ := workspace["id"].(string); id == c.workspaceHint {
				workspaceID = id
				break
			}
		}
	}
	if workspaceID == "" {
		first, _ := workspaces[0].(map[string]any)
		workspaceID, _ = first["id"].(string)
	}
	if workspaceID == "" {
		return "", errors.New("empty workspace id")
	}
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", reauthIssuer)
	headers.Set("Referer", consentURL)
	headers.Set("oai-device-id", c.deviceID)
	resp, err := c.do(ctx, http.MethodPost, reauthIssuer+"/api/accounts/workspace/select",
		map[string]any{"workspace_id": workspaceID}, headers, false)
	if err != nil {
		return "", err
	}
	if isRedirect(resp.StatusCode) {
		location := resp.Header.Get("Location")
		resp.Body.Close()
		if location == "" {
			return "", fmt.Errorf("workspace/select redirect without location (%d)", resp.StatusCode)
		}
		location = absoluteURL(location)
		return extractAuthorizationCode(location, c.expectedState)
	}
	data, err = parseJSONResponse(resp)
	if err != nil {
		return "", err
	}
	orgs := orgsFromData(data)
	next := normalizeContinueURL(continueURLFromJSON(data), consentURL)
	if len(orgs) > 0 && next != "" {
		orgID, _ := orgs[0]["id"].(string)
		projectID := ""
		if projects, _ := orgs[0]["projects"].([]any); len(projects) > 0 {
			if p, _ := projects[0].(map[string]any); p != nil {
				projectID, _ = p["id"].(string)
			}
		}
		orgBody := map[string]any{"org_id": orgID}
		if projectID != "" {
			orgBody["project_id"] = projectID
		}
		headers.Set("Referer", next)
		respOrg, err := c.do(ctx, http.MethodPost, reauthIssuer+"/api/accounts/organization/select", orgBody, headers, false)
		if err != nil {
			return "", err
		}
		if isRedirect(respOrg.StatusCode) {
			location := respOrg.Header.Get("Location")
			respOrg.Body.Close()
			return extractAuthorizationCode(absoluteURL(location), c.expectedState)
		}
		orgData, err := parseJSONResponse(respOrg)
		if err != nil {
			return "", err
		}
		next = normalizeContinueURL(continueURLFromJSON(orgData), next)
		if code, codeErr := extractAuthorizationCode(next, c.expectedState); codeErr != nil || code != "" {
			return code, codeErr
		}
		if next != "" {
			return c.followForCode(ctx, next, next)
		}
		return "", nil
	}
	if code, codeErr := extractAuthorizationCode(next, c.expectedState); codeErr != nil || code != "" {
		return code, codeErr
	}
	if next != "" {
		return c.followForCode(ctx, next, consentURL)
	}
	return "", nil
}

func orgsFromData(data map[string]any) []map[string]any {
	if nested, ok := data["data"].(map[string]any); ok {
		if list, ok := nested["orgs"].([]any); ok {
			var out []map[string]any
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					out = append(out, m)
				}
			}
			return out
		}
	}
	return nil
}

func isRedirect(status int) bool {
	return status >= 300 && status < 400
}

func absoluteURL(raw string) string {
	if strings.HasPrefix(raw, "http") {
		return raw
	}
	return reauthIssuer + raw
}

func extractAuthorizationCode(raw, expectedState string) (string, error) {
	code := extractCodeFromURL(raw)
	if code == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid OAuth callback URL")
	}
	callbackState := u.Query().Get("state")
	if expectedState != "" && callbackState != expectedState {
		return "", errors.New("OAuth state mismatch")
	}
	return code, nil
}

// followForCode manually follows redirects until a callback URL with a code is
// found (the same way a browser would after the consent step).
func (c *reauthClient) followForCode(ctx context.Context, startURL, referer string) (string, error) {
	if code, err := extractAuthorizationCode(startURL, c.expectedState); err != nil || code != "" {
		return code, err
	}
	current := startURL
	headers := http.Header{}
	headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	headers.Set("Upgrade-Insecure-Requests", "1")
	if referer != "" {
		headers.Set("Referer", referer)
	}
	for hop := 0; hop < reauthMaxHops; hop++ {
		u, parseErr := url.Parse(current)
		if parseErr != nil || !allowedOAuthRedirectHost(u.Hostname()) {
			return "", errors.New("refusing OAuth navigation to untrusted host")
		}
		resp, err := c.do(ctx, http.MethodGet, current, nil, headers, false)
		if err != nil {
			return "", err
		}
		final := resp.Request.URL.String()
		if code, codeErr := extractAuthorizationCode(final, c.expectedState); codeErr != nil || code != "" {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			return code, codeErr
		}
		if isRedirect(resp.StatusCode) {
			location := resp.Header.Get("Location")
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			location = absoluteURL(location)
			if code, codeErr := extractAuthorizationCode(location, c.expectedState); codeErr != nil || code != "" {
				return code, codeErr
			}
			current = location
			headers.Set("Referer", final)
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return "", nil
	}
	return "", nil
}

// LoginWithCredentialsForAccount drives the full OpenAI OAuth password login
// flow with a persistent email/password/TOTP secret and returns fresh tokens.
//
// proxyURL, when non-empty, is used as the HTTP(S) proxy; accountID is kept
// for future per-account fingerprinting and is currently unused.
func LoginWithCredentialsForAccount(ctx context.Context, credentials LoginCredentials, proxyURL, workspaceID string) (*TokenResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if credentials.Email == "" || credentials.Password == "" {
		return nil, errors.New("email and password are required")
	}
	client, err := newReauthClient(ctx, proxyURL, workspaceID)
	if err != nil {
		return nil, err
	}
	tokens, err := client.login(ctx, credentials)
	if err != nil {
		return nil, err
	}
	if tokens == nil || tokens.AccessToken == "" {
		return nil, errors.New("login completed without an access token")
	}
	return tokens, nil
}

// login is the actual flow driver.
func (c *reauthClient) login(ctx context.Context, creds LoginCredentials) (*TokenResponse, error) {
	// 1. PKCE + state.
	codeVerifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, err
	}
	codeChallenge := GenerateCodeChallenge(codeVerifier)
	state, err := GenerateState()
	if err != nil {
		return nil, err
	}
	c.expectedState = state
	authorizeParams := url.Values{}
	authorizeParams.Set("response_type", "code")
	authorizeParams.Set("client_id", ClientID)
	authorizeParams.Set("redirect_uri", DefaultRedirectURI)
	authorizeParams.Set("scope", DefaultScopes)
	authorizeParams.Set("state", state)
	authorizeParams.Set("code_challenge", codeChallenge)
	authorizeParams.Set("code_challenge_method", "S256")
	authorizeParams.Set("id_token_add_organizations", "true")
	authorizeParams.Set("codex_cli_simplified_flow", "true")

	sentinel := &openAISentinel{deviceID: c.deviceID, ua: c.ua}

	// 2. Bootstrap the OAuth session so auth.openai.com sets login_session
	// and the client auth cookie.
	authorizeURL := reauthIssuer + "/oauth/authorize?" + authorizeParams.Encode()
	headers := http.Header{}
	headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	headers.Set("Upgrade-Insecure-Requests", "1")
	headers.Set("Referer", "https://chatgpt.com/")
	authResp, err := c.do(ctx, http.MethodGet, authorizeURL, nil, headers, true)
	if err != nil {
		return nil, fmt.Errorf("authorize bootstrap: %w", err)
	}
	io.Copy(io.Discard, io.LimitReader(authResp.Body, 1<<20))
	authResp.Body.Close()
	finalURL := authResp.Request.URL.String()
	if code, codeErr := extractAuthorizationCode(finalURL, c.expectedState); codeErr != nil || code != "" {
		io.Copy(io.Discard, io.LimitReader(authResp.Body, 1<<20))
		authResp.Body.Close()
		if codeErr != nil {
			return nil, codeErr
		}
		return c.exchangeCodeForTokens(ctx, code, codeVerifier)
	}
	if location := authResp.Header.Get("Location"); location != "" {
		if code, codeErr := extractAuthorizationCode(absoluteURL(location), c.expectedState); codeErr != nil || code != "" {
			io.Copy(io.Discard, io.LimitReader(authResp.Body, 1<<20))
			authResp.Body.Close()
			if codeErr != nil {
				return nil, codeErr
			}
			return c.exchangeCodeForTokens(ctx, code, codeVerifier)
		}
	}

	continueReferer := reauthIssuer + "/log-in"
	if strings.HasPrefix(finalURL, reauthIssuer) {
		continueReferer = finalURL
	}

	// 3. Submit the email.
	emailToken, err := sentinel.buildToken(ctx, c.http.Transport, reauthFlowAuthorizeContinue)
	if err != nil {
		return nil, fmt.Errorf("sentinel (authorize_continue): %w", err)
	}
	emailResp, err := c.doJSON(ctx, http.MethodPost, reauthIssuer+"/api/accounts/authorize/continue",
		map[string]any{"username": map[string]any{"kind": "email", "value": creds.Email}},
		continueReferer, emailToken, reauthFlowAuthorizeContinue)
	if err != nil {
		return nil, err
	}
	emailData, err := parseJSONResponse(emailResp)
	if err != nil {
		return nil, err
	}
	if emailResp.StatusCode != http.StatusOK {
		return nil, &reauthHTTPError{StatusCode: emailResp.StatusCode, Body: bodyOf(emailData)}
	}
	next := normalizeContinueURL(continueURLFromJSON(emailData), continueReferer)
	page := pageType(emailData)
	if code, codeErr := extractAuthorizationCode(next, c.expectedState); codeErr != nil || code != "" {
		if codeErr != nil {
			return nil, codeErr
		}
		return c.exchangeCodeForTokens(ctx, code, codeVerifier)
	}

	// Some accounts present MFA immediately after the email step.
	if isMFAChallenge(page, next) {
		after, err := c.completeTOTP(ctx, creds.TOTPSecret, next, emailData)
		if err != nil {
			return nil, err
		}
		next, page = after, ""
	}

	// 4. Consent/workspace shortcut when the flow jumped straight there.
	if consentHint(next, page) {
		consentURL := next
		if !strings.HasPrefix(consentURL, "http") {
			consentURL = reauthIssuer + "/sign-in-with-chatgpt/codex/consent"
		}
		code, err := c.selectWorkspace(ctx, consentURL)
		if err == nil && code != "" {
			return c.exchangeCodeForTokens(ctx, code, codeVerifier)
		}
	}

	// 5. Password verification.
	passwordPageURL := next
	if passwordPageURL == "" {
		passwordPageURL = reauthIssuer + "/log-in/password"
	}
	if err := c.visitPage(ctx, passwordPageURL, continueReferer); err != nil {
		return nil, fmt.Errorf("visit password page: %w", err)
	}
	return c.verifyPassword(ctx, creds, passwordPageURL, continueReferer, codeVerifier, sentinel)
}

// verifyPassword submits the password and then drives MFA / consent to a code.
func (c *reauthClient) verifyPassword(ctx context.Context, creds LoginCredentials, passwordPageURL, continueReferer, codeVerifier string, sentinel *openAISentinel) (*TokenResponse, error) {
	passwordToken, err := sentinel.buildToken(ctx, c.http.Transport, reauthFlowPasswordVerify)
	if err != nil {
		return nil, fmt.Errorf("sentinel (password_verify): %w", err)
	}
	pwdResp, err := c.doJSON(ctx, http.MethodPost, reauthIssuer+"/api/accounts/password/verify",
		map[string]any{"password": creds.Password},
		passwordPageURL, passwordToken, reauthFlowPasswordVerify)
	if err != nil {
		return nil, err
	}
	const maxBody = 2 << 20
	body, readErr := io.ReadAll(io.LimitReader(pwdResp.Body, maxBody))
	pwdResp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	var pwdData map[string]any
	if err := json.Unmarshal(body, &pwdData); err != nil {
		pwdData = map[string]any{"_raw": string(body)}
	}
	// invalid_state is transient; re-visit the password page and retry once.
	if pwdResp.StatusCode == http.StatusConflict && apiError(string(body), "") == "invalid_state" {
		if err := c.visitPage(ctx, passwordPageURL, continueReferer); err != nil {
			return nil, fmt.Errorf("revisit password page: %w", err)
		}
		passwordToken2, err := sentinel.buildToken(ctx, c.http.Transport, reauthFlowPasswordVerify)
		if err != nil {
			return nil, err
		}
		pwdResp2, err := c.doJSON(ctx, http.MethodPost, reauthIssuer+"/api/accounts/password/verify",
			map[string]any{"password": creds.Password},
			passwordPageURL, passwordToken2, reauthFlowPasswordVerify)
		if err != nil {
			return nil, err
		}
		body2, readErr := io.ReadAll(io.LimitReader(pwdResp2.Body, maxBody))
		pwdResp2.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if err := json.Unmarshal(body2, &pwdData); err != nil {
			pwdData = map[string]any{"_raw": string(body2)}
		}
		pwdResp = pwdResp2
	}
	if pwdResp.StatusCode != http.StatusOK {
		return nil, &reauthHTTPError{StatusCode: pwdResp.StatusCode, Body: bodyOf(pwdData)}
	}
	next := continueURLFromJSON(pwdData)
	if next == "" {
		next = passwordPageURL
	}
	next = normalizeContinueURL(next, passwordPageURL)
	page := pageType(pwdData)

	// 6. MFA challenge.
	if isMFAChallenge(page, next) {
		after, err := c.completeTOTP(ctx, creds.TOTPSecret, next, pwdData)
		if err != nil {
			return nil, err
		}
		next, page = after, ""
	}

	// 7. Consent/workspace after password.
	if consentHint(next, page) {
		consentURL := next
		if !strings.HasPrefix(consentURL, "http") {
			consentURL = reauthIssuer + "/sign-in-with-chatgpt/codex/consent"
		}
		if code, err := c.selectWorkspace(ctx, consentURL); err == nil && code != "" {
			return c.exchangeCodeForTokens(ctx, code, codeVerifier)
		}
		if code, err := c.followForCode(ctx, consentURL, passwordPageURL); err == nil && code != "" {
			return c.exchangeCodeForTokens(ctx, code, codeVerifier)
		}
	}

	if next == "" {
		next = reauthIssuer + "/sign-in-with-chatgpt/codex/consent"
	}
	if c.consentAwaits(ctx, next) {
		if err := c.visitPage(ctx, next, passwordPageURL); err != nil {
			return nil, err
		}
	}
	code, err := c.followForCode(ctx, next, passwordPageURL)
	if err != nil {
		return nil, err
	}
	if code == "" {
		return nil, errors.New("no authorization code after login")
	}
	return c.exchangeCodeForTokens(ctx, code, codeVerifier)
}

// consentAwaits reports whether the consent page has completed sequencing so
// the followForCode loop can terminate with a code.
func (c *reauthClient) consentAwaits(ctx context.Context, next string) bool {
	return strings.HasPrefix(next, reauthIssuer+"/sign-in-with-chatgpt")
}

func bodyOf(data map[string]any) string {
	if data == nil {
		return ""
	}
	b, _ := json.Marshal(data)
	return string(b)
}

func isMFAChallenge(pageTypeStr, next string) bool {
	return strings.Contains(strings.ToLower(pageTypeStr), "mfa_challenge") ||
		strings.Contains(strings.ToLower(next), "mfa-challenge")
}

func consentHint(next, page string) bool {
	return strings.Contains(next, "consent") || strings.Contains(next, "sign-in-with-chatgpt") ||
		strings.Contains(next, "workspace") || strings.Contains(next, "organization") ||
		strings.Contains(page, "consent") || strings.Contains(page, "organization")
}

// completeTOTP issues a TOTP challenge, verifies a generated code, and returns
// the continue URL after a successful verification.
func (c *reauthClient) completeTOTP(ctx context.Context, secret, currentURL string, responseData map[string]any) (string, error) {
	if secret == "" {
		return "", errors.New("openai 2FA required but no TOTP secret is configured")
	}
	factor := findTOTPFactor(responseData)
	if factor == nil {
		factor = findTOTPFactor(c.sessionData())
	}
	if factor == nil {
		return "", errors.New("openai MFA list has no TOTP authenticator")
	}
	factorID, _ := factor["id"].(string)
	if factorID == "" {
		return "", errors.New("openai TOTP factor missing id")
	}
	factorURL := reauthIssuer + "/mfa-challenge/" + factorID
	if err := c.visitPage(ctx, factorURL, currentURL); err != nil {
		return "", err
	}
	issueResp, err := c.doJSON(ctx, http.MethodPost, reauthIssuer+"/api/accounts/mfa/issue_challenge",
		map[string]any{"id": factorID, "type": "totp", "force_fresh_challenge": false},
		factorURL, "", "")
	if err != nil {
		return "", err
	}
	issueData, err := parseJSONResponse(issueResp)
	if err != nil {
		return "", err
	}
	if !isSuccessStatus(issueResp.StatusCode) {
		return "", &reauthHTTPError{StatusCode: issueResp.StatusCode, Body: bodyOf(issueData)}
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		return "", fmt.Errorf("generate totp code: %w", err)
	}
	verifyResp, err := c.doJSON(ctx, http.MethodPost, reauthIssuer+"/api/accounts/mfa/verify",
		map[string]any{"id": factorID, "type": "totp", "code": code},
		factorURL, "", "")
	if err != nil {
		return "", err
	}
	verifyData, err := parseJSONResponse(verifyResp)
	if err != nil {
		return "", err
	}
	if verifyResp.StatusCode == http.StatusTooManyRequests {
		return "", &reauthHTTPError{StatusCode: verifyResp.StatusCode, Body: bodyOf(verifyData)}
	}
	if !isSuccessStatus(verifyResp.StatusCode) {
		return "", &reauthHTTPError{StatusCode: verifyResp.StatusCode, Body: bodyOf(verifyData)}
	}
	next := continueURLFromJSON(verifyData)
	if next == "" {
		page := pageType(verifyData)
		switch {
		case strings.Contains(page, "codex") && strings.Contains(page, "consent"):
			next = reauthIssuer + "/sign-in-with-chatgpt/codex/consent"
		case strings.Contains(page, "consent"):
			next = reauthIssuer + "/sign-in-with-chatgpt/consent"
		case strings.Contains(page, "workspace"):
			next = reauthIssuer + "/workspace"
		default:
			next = reauthIssuer + "/sign-in-with-chatgpt/codex/consent"
		}
	}
	next = normalizeContinueURL(next, currentURL)
	if isMFAChallenge(pageType(verifyData), next) {
		return "", errors.New("totp code rejected")
	}
	return next, nil
}

func isSuccessStatus(status int) bool {
	return status >= 200 && status < 300
}
