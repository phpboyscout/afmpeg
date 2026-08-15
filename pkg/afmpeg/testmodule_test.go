package afmpeg_test

import (
	"os"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// Environment variables naming the built ffmpeg-wasi modules the integration
// tests run against. There is one per capability profile (spec 0022) because the
// profiles are not interchangeable: a lean build carries the web-delivery
// essentials, and a test that needs mpegts, libopus, yadif or libass will fail
// against it rather than skip — the engine simply has no such component.
const (
	envModuleLean         = "AFMPEG_TEST_FFMPEG_WASI"
	envModuleIntermediate = "AFMPEG_TEST_FFMPEG_WASI_INTERMEDIATE"
)

// integrationModule resolves the path of a built module that provides profile,
// skipping the test when none was supplied.
//
// Profiles are cumulative, so the resolution is not symmetrical:
//
//   - ProfileLean is satisfied by either variable — an intermediate build is a
//     superset of a lean one, so it can serve a lean test.
//   - ProfileIntermediate is satisfied only by envModuleIntermediate. It
//     deliberately does *not* fall back to envModuleLean: a lean build has no
//     mpegts, libopus, yadif or libass, so falling back would turn "you did not
//     supply that artefact" into a dozen failures whose message is about a
//     missing muxer rather than a missing module. A skip naming the variable is
//     the honest answer, and `just test-integration` sets both.
//
// It returns a path rather than a Runtime because callers construct their own
// with differing options.
func integrationModule(t *testing.T, profile afmpeg.Profile) string {
	t.Helper()

	lean := os.Getenv(envModuleLean)
	intermediate := os.Getenv(envModuleIntermediate)

	switch profile {
	case afmpeg.ProfileIntermediate:
		if intermediate != "" {
			return intermediate
		}

		t.Skipf("set %s to a built intermediate-profile ffmpeg-wasi module to run this test"+
			" (%s is not enough — a lean build lacks these components)",
			envModuleIntermediate, envModuleLean)

	case afmpeg.ProfileLean:
		if lean != "" {
			return lean
		}

		if intermediate != "" {
			return intermediate
		}

		t.Skipf("set %s to a built ffmpeg-wasi module to run this test", envModuleLean)

	case afmpeg.ProfileFull:
		t.Fatalf("integrationModule: no full-profile WASM module exists (spec 0022 §4 — full is native only)")

	default:
		t.Fatalf("integrationModule: unknown profile %q", profile)
	}

	return ""
}
