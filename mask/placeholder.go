package mask

import "regexp"

// Placeholder matches what a Masker writes in place of a value it hides:
// <secret:NAME> for a secret of the workflow, NAME as the workflow lists
// it, and <redacted> for a credential it learned.
var Placeholder = regexp.MustCompile(`<(?:redacted|secret:[^<>\n]+)>`)
