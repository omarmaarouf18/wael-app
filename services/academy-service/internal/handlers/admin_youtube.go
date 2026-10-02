package handlers

import (
	"net/url"
	"regexp"
	"strings"
)

// YouTube id extraction (Phase 4.4). The server never calls YouTube: it only
// parses operator-supplied input and stores the validated 11-character id.

var youtubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// parseYouTubeID extracts a YouTube video id from operator input, accepting:
//   - a bare 11-character id
//   - youtube.com/watch?v= (v parameter in any position, extra params allowed)
//   - youtu.be/<id> (query/fragment ignored)
//   - youtube.com/embed/<id>
//   - youtube.com/shorts/<id>
//
// Only the validated id is returned; anything else is rejected. Hosts are
// limited to youtube.com (with www./m. prefixes) and youtu.be, and only
// http/https schemes are accepted.
func parseYouTubeID(input string) (string, bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", false
	}
	if youtubeIDPattern.MatchString(s) {
		return s, true
	}

	urlStr := s
	if !strings.Contains(urlStr, "://") {
		if !strings.HasPrefix(urlStr, "www.") && !strings.HasPrefix(urlStr, "youtube.") &&
			!strings.HasPrefix(urlStr, "youtu.be") && !strings.HasPrefix(urlStr, "m.youtube") {
			return "", false
		}
		urlStr = "https://" + urlStr
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com":
		switch {
		case u.Path == "/watch":
			return validExtract(u.Query().Get("v"))
		case strings.HasPrefix(u.Path, "/embed/"):
			return validExtract(firstPathSegment(strings.TrimPrefix(u.Path, "/embed/")))
		case strings.HasPrefix(u.Path, "/shorts/"):
			return validExtract(firstPathSegment(strings.TrimPrefix(u.Path, "/shorts/")))
		default:
			return "", false
		}
	case "youtu.be":
		return validExtract(firstPathSegment(strings.TrimPrefix(u.Path, "/")))
	default:
		return "", false
	}
}

func firstPathSegment(p string) string {
	if i := strings.Index(p, "/"); i >= 0 {
		return p[:i]
	}
	return p
}

func validExtract(candidate string) (string, bool) {
	if youtubeIDPattern.MatchString(candidate) {
		return candidate, true
	}
	return "", false
}
