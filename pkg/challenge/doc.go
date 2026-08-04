// Package challenge implements host-neutral appealable BLOCK challenges (#131).
//
// Public classifier / resolver decisions remain exactly ALLOW | BLOCK.
// Challenge is intervention / workflow metadata layered on an appealable
// productivity BLOCK — never a third Stage 2 decision enum value.
//
// Host-neutral core only: no Claude additionalContext delivery (#139), no live
// smoke (#120), no advice consumer (#108), no provider runtime (#132), and no
// exact-assessment cache (#138). Cache identity inputs are exposed for later
// layers without implementing a cache here.
//
// Justification is a bounded external decision summary. This package never
// requests, parses, or persists private chain-of-thought.
package challenge
