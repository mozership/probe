package mask

import "regexp"

// Placeholder matches what a Masker writes in place of a value it hides:
// <secret:NAME> for a secret of the workflow, NAME as the workflow lists
// it, and <redacted> for a credential it learned. A Masker writes NAME as
// it is, so the placeholder of a name with <, > or a line break in it,
// which an environment variable can hardly be given, is not matched, or
// matched in part.
var Placeholder = regexp.MustCompile(`<(?:redacted|secret:[^<>\n]+)>`)
