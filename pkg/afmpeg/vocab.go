package afmpeg

import (
	"context"
	"encoding/json"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

// vocabVersion is the job-spec vocabulary version this afmpeg emits (spec 0007
// §4 contract; roadmap Phase 1 version-gating). It increments additively, once
// per landed vocabulary spec, in merge order:
//
//	1 — baseline + the version gate; no process/probe field changes
//	2 — stream copy / bitstream filters (spec 0013): the "copy" codec sentinel,
//	    "in:type[:idx]" map specifiers, and Output.BitstreamFilters
//	3 — seeking & time ranges (spec 0014): Input.Seek {Start, Mode},
//	    Output.Duration | End (mutually exclusive), Output.CopyTS
//	4 — input options & formats (spec 0024): Input.Format (forced demuxer),
//	    Input.Options (demuxer dict, incl. raw geometry), N:v:K graph selection
//	5 — container coverage (spec 0015): Output.Format (forced muxer),
//	    Output.FormatOptions (muxer dict — segmenting/fragmentation)
//
// Every process/probe spec is stamped with it; the engine rejects a spec whose
// version exceeds what it supports, so a new field can never be silently dropped
// by an older engine (it fails the whole spec instead).
const vocabVersion = 5

// engineVocab is the engine's reply to the op:"version" query — its highest
// supported vocabulary version and the FFmpeg build it links.
type engineVocab struct {
	VocabVersion  int    `json:"vocab_version"`
	FFmpegVersion string `json:"ffmpeg_version"`
}

// evaluateVocab decides whether an engine is new enough for the vocabulary this
// afmpeg emits. gated is true only when the engine answered op:"version" with a
// parseable vocab version; a gated engine older than vocabVersion is rejected. A
// non-gated engine (a pre-gate module, or a generic non-ffmpeg-wasi module) is
// tolerated so Runtime stays able to run any wasm module (spec 0007 §8) — such a
// module simply carries no vocabulary contract to check.
func evaluateVocab(engineVer int, gated bool) error {
	if gated && engineVer < vocabVersion {
		return errors.Newf(
			"afmpeg: ffmpeg-wasi engine vocabulary v%d is older than this afmpeg requires (v%d); upgrade the module",
			engineVer, vocabVersion)
	}

	return nil
}

// preflightVocab queries the engine's vocabulary version once at New and fails
// loudly if the module is a gated engine too old for what afmpeg emits — turning
// a would-be silent field-drop at first job into a clear construction error. The
// query itself carries no version stamp (it is the negotiation channel, so even
// a too-old engine answers it); a non-zero exit or unparseable reply means "not
// a gated engine" and is tolerated.
func (r *Runtime) preflightVocab(ctx context.Context) error {
	// Preflight runs the trusted, pinned module (not untrusted media), so it is
	// bounded only by the caller's New context — not the per-invocation WithTimeout
	// default, which is for media jobs and may be set very short.
	spec, err := json.Marshal(jobSpec{Op: "version"})
	if err != nil {
		return errors.Wrap(err, "afmpeg: marshal version query")
	}

	inv, err := r.invoke(ctx, afero.NewMemMapFs(), string(spec))
	if err != nil {
		return err
	}

	return interpretVocabReply(inv.stdout, inv.exitCode)
}

// interpretVocabReply turns an engine's op:"version" reply into a gate decision.
// A non-zero exit (unknown op on a pre-gate engine) or a reply without a
// parseable vocab version (a generic non-engine module) is tolerated — no
// vocabulary contract to check; otherwise the reported version is evaluated.
func interpretVocabReply(stdout string, exitCode int) error {
	if exitCode != 0 {
		return nil
	}

	var ev engineVocab
	if err := json.Unmarshal([]byte(stdout), &ev); err != nil || ev.VocabVersion == 0 {
		return nil //nolint:nilerr // an unparseable reply means a non-gated module — tolerate, not an error
	}

	return evaluateVocab(ev.VocabVersion, true)
}
