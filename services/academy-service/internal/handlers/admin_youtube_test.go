package handlers

import "testing"

func TestParseYouTubeID_Good(t *testing.T) {
	const want = "dQw4w9WgXcQ"
	good := []string{
		"dQw4w9WgXcQ",
		"  dQw4w9WgXcQ  ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"http://youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?list=PL123&v=dQw4w9WgXcQ&index=2",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ",
		"http://youtu.be/dQw4w9WgXcQ?t=30",
		"https://www.youtube.com/embed/dQw4w9WgXcQ",
		"https://youtube.com/embed/dQw4w9WgXcQ?rel=0",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://youtube.com/shorts/dQw4w9WgXcQ?feature=share",
		"www.youtube.com/watch?v=dQw4w9WgXcQ",
		"youtu.be/dQw4w9WgXcQ",
		"HTTPS://WWW.YOUTUBE.COM/watch?v=dQw4w9WgXcQ",
	}
	for _, in := range good {
		got, ok := parseYouTubeID(in)
		if !ok || got != want {
			t.Errorf("parseYouTubeID(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
}

func TestParseYouTubeID_Bad(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"short",
		"toolongvideoid12345",
		"dQw4w9WgXc!",
		"dQw4 w9WgXcQ",
		"https://vimeo.com/12345678901",
		"https://youtubeevil.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com/watch",
		"https://www.youtube.com/watch?v=short",
		"https://www.youtube.com/watch?v=toolongvideoid12345",
		"https://youtu.be/",
		"https://youtu.be/short",
		"https://www.youtube.com/embed/",
		"https://www.youtube.com/shorts/",
		"https://www.youtube.com/live/dQw4w9WgXcQ",
		"https://www.youtube.com/v/dQw4w9WgXcQ",
		"javascript:alert(dQw4w9WgXcQ)",
		"not a url at all",
		"ftp://youtube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=",
		"https://youtube.com",
		"//youtu.be/dQw4w9WgXcQ/extra/path",
	}
	for _, in := range bad {
		if got, ok := parseYouTubeID(in); ok {
			t.Errorf("parseYouTubeID(%q) = %q, true; want rejected", in, got)
		}
	}
}
