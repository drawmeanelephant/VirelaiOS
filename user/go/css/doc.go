// Package css implements ADR 0028 Amendment D's closed CSS subset.
//
// Design: a byte-positioned tokenizer decodes CSS escapes and separates strings,
// comments, identifiers, numbers and punctuation before parsing. Balanced
// recovery skips declarations and whole rules/at-rules without interpreting their
// contents. Limits are checked before accepting complete sources and constructs.
// Unsupported grouped selectors discard the entire rule; value validation and
// shorthand expansion are atomic, so a rejected declaration cannot overwrite
// any previous longhand winner.
//
// Cascade walks the DOM parent first, retaining one winner per longhand by
// origin/importance, specificity and source order. It applies explicit
// inherit/initial after selecting winners, then resolves inherited fonts/colors.
// Text nodes inherit without matching selectors. The UA stylesheet uses the
// existing tag defaults, expressed only with admitted properties. No DOM mutation,
// resource fetching, renderer-to-css import, or external engine is needed.
package css
