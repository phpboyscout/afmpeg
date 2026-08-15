package native_test

import (
	"os"
	"testing"

	"gitlab.com/phpboyscout/afmpeg/pkg/afmpeg"
)

// A native driver is identified by two independent axes, and a test needs to name
// both: the capability profile (spec 0022 — lean ⊂ intermediate ⊂ full) and the
// licence variant (lgpl ⊂ gpl). Neither is a superset of the other. cropdetect,
// for instance, carries `cropdetect_filter_deps="gpl"` upstream, so it is absent
// from every lgpl build no matter how rich the profile — as is the libx264
// encoder.
//
// Both axes are ordered, so a richer driver satisfies a poorer requirement, and
// the resolver picks the least-rich adequate one that was actually supplied.
// Unlike the WASM modules there is a full profile here: spec 0022 §4 makes full
// native-only.
var nativeDrivers = []struct {
	profile afmpeg.Profile
	gpl     bool
	env     string
}{
	{afmpeg.ProfileLean, false, "AFMPEG_TEST_NATIVE_DRIVER"},
	{afmpeg.ProfileLean, true, "AFMPEG_TEST_NATIVE_DRIVER_GPL"},
	{afmpeg.ProfileIntermediate, false, "AFMPEG_TEST_NATIVE_DRIVER_INTERMEDIATE"},
	{afmpeg.ProfileIntermediate, true, "AFMPEG_TEST_NATIVE_DRIVER_INTERMEDIATE_GPL"},
	{afmpeg.ProfileFull, false, "AFMPEG_TEST_NATIVE_DRIVER_FULL"},
	{afmpeg.ProfileFull, true, "AFMPEG_TEST_NATIVE_DRIVER_FULL_GPL"},
}

// integrationModule resolves a WASM module for the one test here that compares
// the two backends. Only the lean profile is ever needed on this side, so this is
// a narrower thing than pkg/afmpeg's profile-aware resolver — which lives in a
// different test package and cannot be shared without exporting it.
func integrationModule(t *testing.T) string {
	t.Helper()

	// Intermediate is a superset of lean, so either will do.
	for _, env := range []string{"AFMPEG_TEST_FFMPEG_WASI", "AFMPEG_TEST_FFMPEG_WASI_INTERMEDIATE"} {
		if path := os.Getenv(env); path != "" {
			return path
		}
	}

	t.Skip("set AFMPEG_TEST_FFMPEG_WASI to a built ffmpeg-wasi module to run this test")

	return ""
}

// profileRank orders the capability profiles. A higher rank includes everything a
// lower one carries.
func profileRank(t *testing.T, p afmpeg.Profile) int {
	t.Helper()

	switch p {
	case afmpeg.ProfileLean:
		return 0
	case afmpeg.ProfileIntermediate:
		return 1
	case afmpeg.ProfileFull:
		return 2
	default:
		t.Fatalf("unknown profile %q", p)

		return -1
	}
}

// integrationDriver resolves the path of a built native driver providing at least
// profile, and gpl components when needGPL, skipping when none was supplied.
//
// It deliberately does not fall back to an inadequate driver. Running a test
// against a driver missing the very component under test reports a missing filter
// or encoder, which reads as a product defect rather than as the missing artefact
// it actually is — that confusion is what this exists to prevent.
func integrationDriver(t *testing.T, profile afmpeg.Profile, needGPL bool) string {
	t.Helper()

	want := profileRank(t, profile)

	for _, d := range nativeDrivers {
		if profileRank(t, d.profile) < want || (needGPL && !d.gpl) {
			continue
		}

		if path := os.Getenv(d.env); path != "" {
			return path
		}
	}

	variant := "lgpl"
	if needGPL {
		variant = "gpl"
	}

	t.Skipf("no %s/%s native driver supplied — set one of the AFMPEG_TEST_NATIVE_DRIVER* variables"+
		" naming a driver at least that rich", profile, variant)

	return ""
}
