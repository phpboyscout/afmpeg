Feature: Certified release verification
  afmpeg loads an ffmpeg-wasi release only after verifying — against a pinned
  signing key — the KMS signature over its checksums, the module's and
  provenance's checksums, and that provenance names the requested variant
  (spec 0010). Any tamper is rejected with a typed error, before the module runs.

  Background:
    Given a trusted release-signing key
    And a signed "lgpl" release tagged "n8.1.2-3"

  Scenario: a valid release is verified and loaded
    When I load the "lgpl" release
    Then the verified module is returned
    And the reported ffmpeg version is "n8.1.2"

  Scenario: a tampered module is rejected
    Given the release module has been altered after signing
    When I load the "lgpl" release
    Then loading fails with a checksum error

  Scenario: a signature from an untrusted key is rejected
    Given the signing key is not in the trusted set
    When I load the "lgpl" release
    Then loading fails with a signature error

  Scenario: provenance that disagrees with the requested variant is rejected
    Given the provenance names the wrong file for the variant
    When I load the "lgpl" release
    Then loading fails with a provenance error

  Scenario: the intermediate profile is loaded from its own signed asset
    Given a signed "lgpl" intermediate release tagged "n8.1.2-6"
    When I load the "lgpl" intermediate release
    Then the verified module is returned
    And the reported ffmpeg version is "n8.1.2"
