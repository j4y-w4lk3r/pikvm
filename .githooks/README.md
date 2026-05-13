# Repo-tracked git hooks

Hooks in this directory are committed to the repo so the same checks
run on every machine. Activate them once per clone:

```bash
make hooks        # equivalent to: git config core.hooksPath .githooks
```

## Hooks

### `pre-push`
Refuses to push when the GitHub CLI's active account doesn't own (or
isn't a member of an org that owns) the push target. Saves you from
the silent "active gh account is `lso0` but you meant `j4y-w4lk3r`"
class of mistake that surfaces as a confusing 403 only after git
exchanges credentials.

- Parses the push remote's URL (HTTPS or SSH form)
- Calls `gh api user --jq .login` to get the active account
- Falls back to `gh api user/orgs` membership check
- Allows the push when accounts match, blocks with a clear fix otherwise

Bypass when you actually want to override (rare):
```bash
git push --no-verify
```

Skips silently when `gh` isn't installed, when not logged in, or when
the remote isn't on `github.com`.
