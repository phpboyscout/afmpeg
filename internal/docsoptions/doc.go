// Package docsoptions extracts the libav option keys the documentation teaches,
// so they can be checked by shape without an engine (Check) and offered to a real
// driver where one is supplied (the gated test in pkg/afmpeg/native).
//
// The why is afmpeg#11: the option names in docs/ are only wrong at runtime, so
// nothing that compiles or lints them would catch a bad one.
package docsoptions
