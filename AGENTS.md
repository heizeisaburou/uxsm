# Rules for agents working on uxsm

## Attribution: none

Commits belong to the person who runs the repository. No agent, tool or model is
credited anywhere, ever:

- no `Co-Authored-By:` trailer, and no `Signed-off-by:` on an agent's behalf;
- no "Generated with", "Assisted by" or similar line in a commit message, pull
  request, release note, changelog or documentation;
- no agent name in code comments.

This is not about hiding work: the repository is free software, its history
belongs to its author, and a tool is a tool. If a commit message needs to say
that something was generated, it says what generated the *file* and how to
regenerate it, not who typed it.

Before committing, check that the message ends with its last sentence and
nothing else.

## House rules

- **Say why, not what.** Comments and commit messages explain the decision
  behind the code: what a line does is already in the line.
- **English.** Code comments, documentation and every message the program
  prints.
- **No punctuation after a path.** `Wrote /usr/local/share/xsessions/x.desktop`
  and not `…x.desktop.`, so it can be copied from a terminal with a double
  click.
- **`make check` before every commit.** The `pre-commit` hook runs it;
  `make hooks` enables the hooks in a fresh clone.
- **Congruence with uwsm.** uxsm is its X11 counterpart: what uwsm has, uxsm
  has, with the same option letters; what uwsm does not have is left out unless
  there is a reason to differ, and the reason is written down.
