package docsoptions

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Sources are the paths, relative to the module root, whose Markdown is extracted.
var Sources = []string{"README.md", "docs"}

// Dict is the option dictionary a key is written into.
type Dict int

// The dictionaries a documented key can land in.
const (
	EncoderDict Dict = iota
	VideoDict
	AudioDict
	SubtitleDict
	MuxerDict
	DemuxerDict
)

func (d Dict) String() string {
	switch d {
	case EncoderDict:
		return "encoder"
	case VideoDict:
		return "video encoder"
	case AudioDict:
		return "audio encoder"
	case SubtitleDict:
		return "subtitle encoder"
	case MuxerDict:
		return "muxer"
	case DemuxerDict:
		return "demuxer"
	default:
		return fmt.Sprintf("Dict(%d)", int(d))
	}
}

// IsEncoder reports whether the dictionary reaches an encoder.
func (d Dict) IsEncoder() bool {
	return d == EncoderDict || d == VideoDict || d == AudioDict || d == SubtitleDict
}

// SiteKind says what a Site's keys were written inside.
type SiteKind int

// The places a documented key can be written.
const (
	// Unscoped keys sit in a fragment with no enclosing input or output, such as
	// an inline code span in prose.
	Unscoped SiteKind = iota
	OutputSite
	InputSite
)

// Key is one documented option.
type Key struct {
	Dict  Dict
	Name  string
	Value string
	Line  int
}

// Site is one documented input or output and the option keys written into it,
// with what the docs set alongside them.
type Site struct {
	File       string
	Line       int
	Kind       SiteKind
	Path       string
	Format     string
	VideoCodec string
	AudioCodec string
	Keys       []Key
}

// Extract reads every Markdown file under Sources in fsys and returns the
// documented sites that carry at least one option key.
func Extract(fsys fs.FS) ([]Site, error) {
	var sites []Site

	for _, src := range Sources {
		err := fs.WalkDir(fsys, src, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() || path.Ext(name) != ".md" {
				return nil
			}

			b, err := fs.ReadFile(fsys, name)
			if err != nil {
				return err
			}

			sites = append(sites, ExtractFile(name, string(b))...)

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("docsoptions: walk %s: %w", src, err)
		}
	}

	return sites, nil
}

// ExtractFile returns the sites in one Markdown document. Fenced code blocks and
// inline code spans are each read as an independent fragment.
func ExtractFile(name, md string) []Site {
	frags := fragments(md)
	sites := make([]Site, 0, len(frags))

	for _, f := range frags {
		sites = append(sites, extractFragment(name, f)...)
	}

	return sites
}

type fragment struct {
	text string
	line int
}

var (
	inlineSpan = regexp.MustCompile("`+(?P<body>[^`]+)`+")
	inlineBody = inlineSpan.SubexpIndex("body")
)

func fragments(md string) []fragment {
	var (
		out     []fragment
		fence   strings.Builder
		fenceAt int
		inFence bool
	)

	for i, line := range strings.Split(md, "\n") {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		isFence := strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")

		switch {
		case isFence && inFence:
			out = append(out, fragment{text: strings.TrimSuffix(fence.String(), "\n"), line: fenceAt})
			inFence = false

			fence.Reset()
		case isFence:
			inFence, fenceAt = true, lineNo+1
		case inFence:
			fence.WriteString(line + "\n")
		default:
			for _, m := range inlineSpan.FindAllStringSubmatch(line, -1) {
				out = append(out, fragment{text: m[inlineBody], line: lineNo})
			}
		}
	}

	return out
}

var (
	optionCall = regexp.MustCompile(`\b(?P<fn>WithOption|EncoderOption|VideoOption|AudioOption|SubtitleOption|FormatOption|DemuxerOption)` +
		`\(\s*"(?P<key>[^"]*)"\s*,\s*(?:"(?P<value>[^"]*)")?`)
	optionMap = regexp.MustCompile(
		`(?P<input>\bInput\.)?\b(?P<kind>Video|Audio|Subtitle|Format)?Options\s*:?\s*(?:map\[string\]string\s*)?\{`)
	mapEntry = regexp.MustCompile(`"(?P<key>[^"]*)"\s*:\s*(?:"(?P<value>[^"]*)")?`)

	callFn, callKey, callValue = optionCall.SubexpIndex("fn"), optionCall.SubexpIndex("key"), optionCall.SubexpIndex("value")
	mapInput, mapKindName      = optionMap.SubexpIndex("input"), optionMap.SubexpIndex("kind")
	entryKey, entryValue       = mapEntry.SubexpIndex("key"), mapEntry.SubexpIndex("value")
)

var callDict = map[string]Dict{
	"WithOption":     EncoderDict,
	"EncoderOption":  EncoderDict,
	"VideoOption":    VideoDict,
	"AudioOption":    AudioDict,
	"SubtitleOption": SubtitleDict,
	"FormatOption":   MuxerDict,
	"DemuxerOption":  DemuxerDict,
}

var mapDict = map[string]Dict{
	"Video":    VideoDict,
	"Audio":    AudioDict,
	"Subtitle": SubtitleDict,
	"Format":   MuxerDict,
}

func extractFragment(name string, f fragment) []Site {
	masked := mask(f.text)
	byScope := map[int]*Site{}

	add := func(at int, dict func(SiteKind) Dict, k, v string) {
		sc := enclosingScope(masked, at)

		site, ok := byScope[sc.start]
		if !ok {
			site = describe(name, f, sc)
			byScope[sc.start] = site
		}

		site.Keys = append(site.Keys, Key{Dict: dict(sc.kind), Name: k, Value: v, Line: lineOf(f, at)})
	}

	for _, m := range optionCall.FindAllStringSubmatchIndex(f.text, -1) {
		if masked[m[0]] != f.text[m[0]] {
			continue // inside a string or comment
		}

		d := callDict[group(f.text, m, callFn)]
		add(m[0], func(SiteKind) Dict { return d }, group(f.text, m, callKey), group(f.text, m, callValue))
	}

	for _, m := range optionMap.FindAllStringSubmatchIndex(f.text, -1) {
		if masked[m[0]] != f.text[m[0]] {
			continue
		}

		dict := mapKind(group(f.text, m, mapInput) != "", group(f.text, m, mapKindName))
		open := m[1] - 1
		body := f.text[open:matching(masked, open)]

		for _, e := range mapEntry.FindAllStringSubmatchIndex(body, -1) {
			add(m[0], dict, group(body, e, entryKey), group(body, e, entryValue))
		}
	}

	sites := make([]Site, 0, len(byScope))
	for _, s := range byScope {
		sort.SliceStable(s.Keys, func(i, j int) bool { return s.Keys[i].Line < s.Keys[j].Line })
		sites = append(sites, *s)
	}

	sort.Slice(sites, func(i, j int) bool { return sites[i].Line < sites[j].Line })

	return sites
}

func mapKind(inputQualified bool, prefix string) func(SiteKind) Dict {
	if inputQualified {
		return func(SiteKind) Dict { return DemuxerDict }
	}

	if d, ok := mapDict[prefix]; ok {
		return func(SiteKind) Dict { return d }
	}

	return func(k SiteKind) Dict {
		if k == InputSite {
			return DemuxerDict
		}

		return EncoderDict
	}
}

func group(s string, m []int, n int) string {
	if m[2*n] < 0 {
		return ""
	}

	return s[m[2*n]:m[2*n+1]]
}

func lineOf(f fragment, at int) int {
	return f.line + strings.Count(f.text[:at], "\n")
}

// mask blanks string-literal bodies and line comments, keeping every offset, so
// brackets inside a filtergraph string or a comment cannot unbalance the scope
// walk.
func mask(s string) string {
	b := []byte(s)

	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '"':
			i = blankString(b, i+1)
		case bytes.HasPrefix(b[i:], []byte("//")):
			for ; i < len(b) && b[i] != '\n'; i++ {
				b[i] = ' '
			}
		}
	}

	return string(b)
}

// blankString overwrites a string literal's body from i and returns the index of
// its closing quote.
func blankString(b []byte, i int) int {
	for ; i < len(b) && b[i] != '"'; i++ {
		if b[i] == '\\' && i+1 < len(b) {
			b[i] = 'x'
			i++
		}

		b[i] = 'x'
	}

	return i
}

func matching(masked string, open int) int {
	depth := 0

	for i := open; i < len(masked); i++ {
		switch masked[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}

	return len(masked)
}

// outerOpener returns the index of the unmatched bracket that encloses at, or -1.
func outerOpener(masked string, at int) int {
	depth := 0

	for i := at - 1; i >= 0; i-- {
		switch masked[i] {
		case ')', '}', ']':
			depth++
		case '(', '{', '[':
			if depth == 0 {
				return i
			}

			depth--
		}
	}

	return -1
}

type scope struct {
	kind       SiteKind
	start, end int
}

var (
	callee    = regexp.MustCompile(`(\w+)\s*$`)
	sliceType = regexp.MustCompile(`\[\]\s*(?:\w+\.)?(Input|Output)\s*$`)
	namedType = regexp.MustCompile(`(?:^|[^\]\w.])\s*(?:\w+\.)?(Input|Output)\s*$`)
)

// enclosingScope finds the innermost Input or Output that at sits in: a
// WithInput/WithOutput call or an Input/Output composite literal, including an
// element literal whose type is elided inside a []Input or []Output.
func enclosingScope(masked string, at int) scope {
	elided := -1

	for i := outerOpener(masked, at); i >= 0; i = outerOpener(masked, i) {
		before := masked[:i]

		if kind, ok := openerKind(masked[i], before); ok {
			return scope{kind: kind, start: i, end: matching(masked, i)}
		}

		if masked[i] != '{' {
			continue
		}

		if m := sliceType.FindStringSubmatch(before); m != nil && elided >= 0 {
			return scope{kind: siteKind(m[1]), start: elided, end: matching(masked, elided)}
		}

		if t := strings.TrimRight(before, " \t\n"); strings.HasSuffix(t, "{") || strings.HasSuffix(t, ",") {
			elided = i
		}
	}

	return scope{kind: Unscoped, start: -1, end: len(masked)}
}

func openerKind(opener byte, before string) (SiteKind, bool) {
	switch opener {
	case '(':
		m := callee.FindStringSubmatch(before)
		if m == nil {
			return Unscoped, false
		}

		switch m[1] {
		case "WithOutput":
			return OutputSite, true
		case "WithInput":
			return InputSite, true
		}
	case '{':
		if sliceType.MatchString(before) {
			return Unscoped, false
		}

		if m := namedType.FindStringSubmatch(before); m != nil {
			return siteKind(m[1]), true
		}
	}

	return Unscoped, false
}

func siteKind(name string) SiteKind {
	if name == "Input" {
		return InputSite
	}

	return OutputSite
}

var (
	pathField   = regexp.MustCompile(`\bPath:\s*"([^"]*)"`)
	firstString = regexp.MustCompile(`^\(\s*"([^"]*)"`)
	formatSet   = regexp.MustCompile(`\b(?:OutputFormat\(|InputFormat\(|Format:)\s*"([^"]*)"`)
	videoCodec  = regexp.MustCompile(`\bVideoCodec(?:\(|:)\s*"([^"]*)"`)
	audioCodec  = regexp.MustCompile(`\bAudioCodec(?:\(|:)\s*"([^"]*)"`)
)

func describe(name string, f fragment, sc scope) *Site {
	site := &Site{File: name, Line: f.line, Kind: sc.kind}
	if sc.kind == Unscoped {
		return site
	}

	text := f.text[sc.start:sc.end]
	site.Line = lineOf(f, sc.start)
	site.Path = firstSub(pathField, text)

	if site.Path == "" {
		site.Path = firstSub(firstString, text)
	}

	site.Format = firstSub(formatSet, text)
	site.VideoCodec = firstSub(videoCodec, text)
	site.AudioCodec = firstSub(audioCodec, text)

	return site
}

func firstSub(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}

	return ""
}
