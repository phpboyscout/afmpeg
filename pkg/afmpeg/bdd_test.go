package afmpeg

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/openpgpkey"
	"gitlab.com/phpboyscout/go/signing/verify"
)

// releaseWorld is the per-scenario state for the release-verification feature.
type releaseWorld struct {
	priv    *rsa.PrivateKey
	pub     []byte   // the signer's armored public key
	trusted [][]byte // the keys afmpeg is told to trust (default: pub)
	dir     string
	tag     string
	module  []byte
	gotMod  []byte
	prov    Provenance
	err     error
}

func (w *releaseWorld) aTrustedKey() error {
	priv, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return err
	}

	pub, err := openpgpkey.ArmoredPublicKey(priv, "ffmpeg-wasi Release", "ffmpeg-wasi-release@phpboyscout.uk", testKeyEpoch)
	if err != nil {
		return err
	}

	w.priv, w.pub, w.trusted = priv, pub, [][]byte{pub}

	return nil
}

// writeRelease assembles a signed release for variant into the scenario's dir. If
// wrongProvFile, provenance names the wrong file for the lgpl variant.
func (w *releaseWorld) writeRelease(variant string, wrongProvFile bool) error {
	moduleFile := "ffmpeg-wasi-" + variant + ".wasm"
	w.module = []byte("\x00asm bdd " + variant)

	lgplFile := "ffmpeg-wasi-lgpl.wasm"
	if wrongProvFile {
		lgplFile = "ffmpeg-wasi-gpl.wasm"
	}

	prov := Provenance{
		FFmpegVersion:  "n8.1.2",
		BuildTag:       w.tag,
		ToolingLicense: "MIT",
		Variants: map[string]ProvenanceVariant{
			"lgpl": {File: lgplFile, License: "LGPL-2.1-or-later", H264Encode: "openh264"},
			"gpl":  {File: "ffmpeg-wasi-gpl.wasm", License: "GPL-2.0-or-later", H264Encode: "libx264"},
		},
	}

	return w.signAndWrite(moduleFile, prov)
}

// writeIntermediateRelease assembles a signed intermediate-profile release: the
// module is ffmpeg-wasi-intermediate-<variant>.wasm and provenance carries both
// the lean and the intermediate-<variant> entries, as a real release does.
func (w *releaseWorld) writeIntermediateRelease(variant string) error {
	moduleFile := "ffmpeg-wasi-intermediate-" + variant + ".wasm"
	w.module = []byte("\x00asm bdd intermediate " + variant)

	prov := Provenance{
		FFmpegVersion:  "n8.1.2",
		BuildTag:       w.tag,
		ToolingLicense: "MIT",
		Variants: map[string]ProvenanceVariant{
			"lgpl":                    {File: "ffmpeg-wasi-lgpl.wasm", License: "LGPL-2.1-or-later", H264Encode: "openh264", Profile: "lean"},
			"intermediate-" + variant: {File: moduleFile, License: "LGPL-2.1-or-later", H264Encode: "openh264", Profile: "intermediate"},
		},
	}

	return w.signAndWrite(moduleFile, prov)
}

// signAndWrite signs prov + the module into a checksums manifest and writes the
// full bundle (module, checksums.txt, checksums.txt.sig, provenance.json) to the
// scenario dir.
func (w *releaseWorld) signAndWrite(moduleFile string, prov Provenance) error {
	provBytes, err := json.Marshal(prov)
	if err != nil {
		return err
	}

	checksums := buildChecksums(map[string][]byte{moduleFile: w.module, "provenance.json": provBytes})

	sig, err := openpgpkey.DetachSign(w.priv, w.pub, bytes.NewReader(checksums), testKeyEpoch)
	if err != nil {
		return err
	}

	for name, data := range map[string][]byte{
		moduleFile:          w.module,
		"checksums.txt":     checksums,
		"checksums.txt.sig": sig,
		"provenance.json":   provBytes,
	} {
		if err := os.WriteFile(filepath.Join(w.dir, name), data, 0o644); err != nil {
			return err
		}
	}

	return nil
}

func (w *releaseWorld) aSignedRelease(variant, tag string) error {
	w.tag = tag

	return w.writeRelease(variant, false)
}

func (w *releaseWorld) aSignedIntermediateRelease(variant, tag string) error {
	w.tag = tag

	return w.writeIntermediateRelease(variant)
}

func (w *releaseWorld) alterModule() error {
	return os.WriteFile(filepath.Join(w.dir, "ffmpeg-wasi-lgpl.wasm"), append(w.module, 'x'), 0o644)
}

func (w *releaseWorld) untrustKey() error {
	other, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return err
	}

	otherPub, err := openpgpkey.ArmoredPublicKey(other, "Someone Else", "someone@example.test", testKeyEpoch)
	if err != nil {
		return err
	}

	w.trusted = [][]byte{otherPub}

	return nil
}

func (w *releaseWorld) wrongProvenance() error {
	return w.writeRelease("lgpl", true)
}

func (w *releaseWorld) loadRelease(variant string) error {
	cfg := &config{}

	opt := WithModuleRelease(w.tag, Variant(variant),
		WithReleaseBundleDir(w.dir),
		withReleaseKeys(w.trusted...),
		WithReleaseProvenance(&w.prov),
	)
	if err := opt(cfg); err != nil {
		w.err = err // captured for the "Then loading fails" assertion

		return nil //nolint:nilerr // the step succeeds; the option error is the subject under test
	}

	w.gotMod, w.err = cfg.fetch(context.Background())

	return nil
}

func (w *releaseWorld) loadIntermediateRelease(variant string) error {
	cfg := &config{}

	opt := WithModuleRelease(w.tag, Variant(variant),
		WithReleaseBundleDir(w.dir),
		withReleaseKeys(w.trusted...),
		WithReleaseProfile(ProfileIntermediate),
		WithReleaseProvenance(&w.prov),
	)
	if err := opt(cfg); err != nil {
		w.err = err // captured for the "Then loading fails" assertion

		return nil //nolint:nilerr // the step succeeds; the option error is the subject under test
	}

	w.gotMod, w.err = cfg.fetch(context.Background())

	return nil
}

func (w *releaseWorld) moduleReturned() error {
	if w.err != nil {
		return errors.Wrap(w.err, "expected the module to load")
	}

	if !bytes.Equal(w.gotMod, w.module) {
		return errors.New("returned module bytes differ from the signed module")
	}

	return nil
}

func (w *releaseWorld) ffmpegVersionIs(want string) error {
	if w.prov.FFmpegVersion != want {
		return errors.Newf("ffmpeg version = %q, want %q", w.prov.FFmpegVersion, want)
	}

	return nil
}

func (w *releaseWorld) failsWith(sentinel error) error {
	if !errors.Is(w.err, sentinel) {
		return errors.Newf("error = %v, want %v", w.err, sentinel)
	}

	return nil
}

func (w *releaseWorld) failsChecksum() error   { return w.failsWith(ErrChecksumMismatch) }
func (w *releaseWorld) failsSignature() error  { return w.failsWith(verify.ErrSignatureInvalid) }
func (w *releaseWorld) failsProvenance() error { return w.failsWith(ErrProvenanceMismatch) }

// TestReleaseVerificationFeatures runs the Gherkin trust-contract scenarios — the
// security guarantees of spec 0010 expressed as executable specification.
func TestReleaseVerificationFeatures(t *testing.T) {
	t.Parallel()

	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			w := &releaseWorld{}

			sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				dir, err := os.MkdirTemp("", "afmpeg-bdd-")
				if err != nil {
					return ctx, err
				}

				*w = releaseWorld{dir: dir}

				return ctx, nil
			})
			sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
				_ = os.RemoveAll(w.dir)

				return ctx, nil
			})

			sc.Step(`^a trusted release-signing key$`, w.aTrustedKey)
			sc.Step(`^a signed "([^"]*)" release tagged "([^"]*)"$`, w.aSignedRelease)
			sc.Step(`^a signed "([^"]*)" intermediate release tagged "([^"]*)"$`, w.aSignedIntermediateRelease)
			sc.Step(`^the release module has been altered after signing$`, w.alterModule)
			sc.Step(`^the signing key is not in the trusted set$`, w.untrustKey)
			sc.Step(`^the provenance names the wrong file for the variant$`, w.wrongProvenance)
			sc.Step(`^I load the "([^"]*)" release$`, w.loadRelease)
			sc.Step(`^I load the "([^"]*)" intermediate release$`, w.loadIntermediateRelease)
			sc.Step(`^the verified module is returned$`, w.moduleReturned)
			sc.Step(`^the reported ffmpeg version is "([^"]*)"$`, w.ffmpegVersionIs)
			sc.Step(`^loading fails with a checksum error$`, w.failsChecksum)
			sc.Step(`^loading fails with a signature error$`, w.failsSignature)
			sc.Step(`^loading fails with a provenance error$`, w.failsProvenance)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("release-verification BDD scenarios failed")
	}
}
