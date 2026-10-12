package probe

import (
	_ "embed"
	"fmt"
	"io"
	"path/filepath"

	"github.com/mozership/probe/safefile"
)

// skill is the agent skill in skills/probe/SKILL.md. It lives there so that
// skill installers that read a repository find it, and it is built into the
// binary so that probe skill installs the copy that matches this version.
//
//go:embed skills/probe/SKILL.md
var skill string

// DefaultSkillDir is where probe skill install puts the skill when no
// directory is given: the project-level skills directory of Claude Code.
const DefaultSkillDir = ".claude/skills/probe"

// Skill returns the agent skill as Markdown.
func Skill() string {
	return skill
}

// InstallSkill writes SKILL.md into dir, creating it, and returns the path it
// wrote. An existing SKILL.md is replaced, so running it again after an
// upgrade brings the skill up to date.
//
// The default directory is inside the project, which the user may have
// checked out from anywhere, so the file is written through safefile.Replace: a
// symlink at SKILL.md, or at .claude or another directory on the way, cannot
// redirect it to a file elsewhere.
func InstallSkill(dir string) (string, error) {
	if dir == "" {
		dir = DefaultSkillDir
	}
	path := filepath.Join(dir, "SKILL.md")
	err := safefile.Replace(path, func(w io.Writer) error {
		_, err := io.WriteString(w, skill)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("failed to write the skill: %w", err)
	}
	return path, nil
}
