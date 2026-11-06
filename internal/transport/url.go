package transport

import "net/url"

// RedactURLQuery masks every query parameter value, keeping the names, so a URL
// carrying a credential can be printed. A malformed URL is returned unchanged.
func RedactURLQuery(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.RawQuery == "" {
		return rawURL
	}
	q := u.Query()
	for k := range q {
		q[k] = []string{"***"}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
