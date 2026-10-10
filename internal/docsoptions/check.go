package docsoptions

import (
	"fmt"
	"strings"
)

// Each name below appears in no option table on the other side, checked against
// a full `ffmpeg -h full` dump. A name both sides have, such as `window_size`,
// cannot be judged by name alone and is left out.
var (
	muxerOnly = set(
		"movflags", "brand", "write_tmcd", "frag_duration", "frag_size",
		"hls_time", "hls_init_time", "hls_list_size", "hls_segment_filename", "hls_segment_type",
		"hls_flags", "hls_playlist_type", "hls_fmp4_init_filename", "hls_base_url",
		"hls_start_number_source", "master_pl_name", "var_stream_map",
		"seg_duration", "use_template", "use_timeline", "extra_window_size",
		"init_seg_name", "media_seg_name",
		"segment_time", "segment_format", "segment_list", "segment_list_type", "reset_timestamps",
		"mpegts_flags",
	)
	encoderOnly = set(
		"b", "g", "bf", "crf", "qp", "preset", "tune", "maxrate", "minrate", "bufsize",
		"qmin", "qmax", "keyint_min", "x264-params", "x265-params", "svtav1-params",
	)
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}

	return m
}

// Violation is a documented key that cannot be right whatever engine runs it.
type Violation struct {
	File   string
	Key    Key
	Reason string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s:%d: %q in the %s dictionary: %s", v.File, v.Key.Line, v.Key.Name, v.Key.Dict, v.Reason)
}

// Check applies the shape rules every documented key must pass: no command-line
// stream specifier, and no name that only the other kind of component has.
func Check(sites []Site) []Violation {
	var out []Violation

	for _, s := range sites {
		for _, k := range s.Keys {
			if reason := judge(k); reason != "" {
				out = append(out, Violation{File: s.File, Key: k, Reason: reason})
			}
		}
	}

	return out
}

func judge(k Key) string {
	switch {
	case strings.Contains(k.Name, ":"):
		return "contains ':', a command-line stream specifier that libav never sees; drop it and address the option with VideoOption, AudioOption or SubtitleOption"
	case muxerOnly[k.Name] && k.Dict != MuxerDict:
		return "is a muxer option; set it with FormatOption or FormatOptions"
	case encoderOnly[k.Name] && !k.Dict.IsEncoder():
		return "is an encoder option; set it with VideoOption, AudioOption or EncoderOption"
	}

	return ""
}
