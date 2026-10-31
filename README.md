# Godiff

A native, minimal, local diff viewer for reviewing Git changes and committing
them: a reimplementation of [codiff](https://github.com/nkzw-tech/codiff) in Go
with [MyGo](https://mygo.egoist.dev/)'s native UI. No webview, no JavaScript:
the window is drawn by MyGo on the GPU, with the system's fonts, accent color,
dark mode, menus and vibrancy.

<img width="1222" height="844" alt="截屏 2026-10-05 at 00 12 00 (2)" src="https://github.com/user-attachments/assets/a665076b-af70-4c70-b487-fe93b70cfa71" />


## Features

- **Review local changes**: staged, unstaged and untracked files against
  `HEAD`, in one scrolling surface of file cards with sticky headers.
- **Staging**: collapsible Staged changes and Changes trees, with file
  counts and per-file or bulk stage/unstage actions in each repository.
  Open either group's diff with its review button, including partially staged
  files. Discarding unstaged edits asks for confirmation and keeps the
  index; directories and subrepository contents are left to their own tools.
- **Subrepositories**: local submodules and nested Git repositories appear
  as compact, collapsible sections in Files, History and Pull Requests.
  Each repository keeps its own selected source, filters, comments, AI
  review and commit draft. Repository headers only expand or collapse their
  groups. Select a file, commit or pull request, or use the command bar, to
  switch repositories; refresh updates all repositories and their history.
  Hover or focus a header to review its local changes, open its remote
  branch, or refresh only that repository.
- **Split and unified diffs** with syntax highlighting (Licht in light mode
  and VS Code's Dark Modern in dark mode), word-level change highlighting,
  and unchanged lines that expand 100 at a time or all at once. New and
  deleted files show in one column.
- **Viewed files**: marking a file viewed collapses it, and it opens again
  once it changes. Generated files (lockfiles, `*.min.js`,
  `linguist-generated`, …) and dependency folders start collapsed.
- **Review comments**: click a line, or press <kbd>J</kbd>/<kbd>K</kbd> to
  choose a hunk and <kbd>Enter</kbd>, then copy every comment as Markdown,
  with the diff around it, for an agent or a colleague.
- **Commit** the files you choose, with a subject and a summary, from
  **File → Commit…** or the command bar. Other staged work stays staged.
- **History**: browse commits and review any of them, or compare the work
  tree with a branch.
- **Pull requests**, after [pulls.review](https://github.com/antfu/pulls.review):
  list the open pull requests of the repository's GitHub remote
  (<kbd>⌘3</kbd>) and review any of them, open or not. Godiff fetches its
  commits into your clone, so it reads as local changes do: highlighted,
  expandable, with pictures. Its files group by kind (code, tests, docs,
  config, dependencies, generated), each group with how much of it you
  viewed; review threads show under their lines, to reply to and resolve;
  comments go out alone or into your review, which you submit as a comment,
  an approval or a request for changes. A banner says when new commits are
  pushed.
- **AI review**: a coding agent installed on your machine — Claude Code,
  Codex, OpenCode or Pi — reviews the changes, a pull request's or your
  own: it summarizes them, groups the files by intent, and notes the lines
  that deserve attention, marking what needs care. It runs read-only, with
  the agent's own sign-in, and its review is kept until you ask for
  another. Choose the agent and the model in the review's card, among
  those the agent lists, of any of its providers; by default OpenCode
  reviews with the model it starts with itself. The card lists the notes,
  and the file tree marks the files with some: click one, or press
  <kbd>N</kbd> and <kbd>⇧N</kbd>, to go from note to note.
- **Find in diffs** (<kbd>⌘F</kbd>), a **file filter** (<kbd>⌘P</kbd>) and a
  **command bar** (<kbd>⌘K</kbd>).
- Image previews of changed pictures, a file tree with change counts and
  status letters, and a banner when the work tree changes.
- Sidebar navigation selects files, directories, commits and pull requests
  on pointer press, showing the new selection as the sidebar takes focus.
  Directory disclosure and button actions activate on release. The sidebar
  starts on the right; **View → Sidebar Position** switches it to either side.
- **File and folder icons** from [Material Icon Theme](https://github.com/material-extensions/vscode-material-icon-theme),
  with original colors, light-mode variants and specialized file-name associations.

## Usage

```sh
go run . [<commit> | <branch> | <pull request>] [<path>]
```

- `godiff` reviews the uncommitted changes of the repository you are in.
- `godiff HEAD~1` reviews a commit, against its first parent.
- `godiff main` compares the work tree, committed or not, with where it
  branched off `main`.
- `godiff --pr 123`, `godiff owner/repo#123` or `godiff
  https://github.com/owner/repo/pull/123` reviews a pull request of the
  repository's GitHub remote (`upstream`, else `origin`).
- `godiff ../other-repo` opens another repository.

Opening a repository creates a window for it and its local subrepositories.
**Godiff → Install Command Line Tool…** installs a `godiff` command that opens
the app on the repository it runs in. Started from Finder or the Dock, the app
reopens the repository opened
last, and **File → Open Recent** lists the ten opened lately.

### Keyboard

| Keys | |
|---|---|
| <kbd>⌘K</kbd> | Command bar |
| <kbd>⌘P</kbd> | Filter files |
| <kbd>⌘F</kbd> | Find in diffs |
| <kbd>J</kbd> / <kbd>K</kbd> | Next / previous hunk |
| <kbd>N</kbd> / <kbd>⇧N</kbd> | Next / previous AI note |
| <kbd>Enter</kbd> | Comment on the hunk chosen |
| <kbd>⌘↩</kbd> | Add the comment; commit, in the commit view |
| <kbd>⌘⇧B</kbd> | Toggle the sidebar |
| <kbd>⌘1</kbd> / <kbd>⌘2</kbd> / <kbd>⌘3</kbd> | Files / History / Pull requests |
| <kbd>⌘⇧I</kbd> | Review with AI |
| <kbd>⌥Z</kbd> | Toggle word wrap |
| <kbd>⌘⇧O</kbd> | Open the file in your editor |
| <kbd>⌘R</kbd> | Refresh the changes |
| <kbd>⌘+</kbd> / <kbd>⌘-</kbd> / <kbd>⌘0</kbd> | Code font size |
| <kbd>⇧?</kbd> | All the shortcuts |

## Configuration

Settings live in `~/.godiff/godiff.jsonc` (**Godiff → Open Config File…**),
with codiff's names, and apply to open windows as the file changes:

```jsonc
{
  "settings": {
    "codeFontFamily": "",          // e.g. "JetBrains Mono"; empty is SF Mono
    "codeFontSize": 13,
    "copyCommentsOnClose": false,
    "diffStyle": "split",          // or "unified"
    "editorCommand": "",           // e.g. "zed {file}:{line}"
    "githubToken": "",             // empty uses the GitHub CLI's (gh auth login)
    "aiAgent": "",                 // claude, codex, opencode or pi; empty is the first installed
    "aiModel": "",                 // e.g. "sonnet" or "deepseek/deepseek-flash"; empty is the agent's default
    "reviewCommentsPrefix": "# Address these Review Comments",
    "sidebarPosition": "right",    // or "left"
    "showWhitespace": false,
    "theme": "system",             // or "light", "dark"
    "wordWrap": false
  }
}
```

Files open in `$GODIFF_EDITOR` or `editorCommand` (`{file}`, `{line}` and
`{repo}` are replaced), else VS Code, else the app the system opens them with.

Pull requests need no token to be read when they are public; to comment
and review, or to read private ones, sign in with the GitHub CLI (`gh auth
login`), or set `githubToken` or `$GH_TOKEN`.

## Development

```sh
go tool mygo dev          # the app, rebuilt as you edit
go test ./...             # including the views, run without a window
go tool mygo build        # Godiff.app and a disk image in build/
go run ./tools/genicon    # render resources/icon.svg to the app icon
go run ./tools/genfileicons <material-icon-theme package>  # update the file icons
```

`GODIFF_SNAPSHOTS=<dir> go test .` saves PNGs of the views the tests drive, and
`GODIFF_CAPTURE=<file.png>` makes the app save a picture of its window once
loaded, then quit. `GODIFF_DEBUG=1` logs every git command with its duration, the
frames that take more than 4ms to build, and whenever the main thread keeps
work waiting over 30ms. `GODIFF_NAME=<name>` runs a build apart from the
installed app, which would otherwise take its windows, and
`GODIFF_STARTUP_PROFILE=<file>` writes a CPU profile of the first seconds.

The code is in a few parts:

- `internal/git`: the repository through the `git` command: changes,
  history, commits, generated files, file contents (`git cat-file --batch`).
- `internal/diff`: parsing patches, and word-level differences.
- `internal/highlight`: syntax highlighting with Chroma.
- The `main` package: the window and its views (`view.go`, `sidebar.go`,
  `diffview.go`, `commit.go`, `palette.go`), the rows of the diff surface
  (`rows.go`), comments, find, settings and menus.

## Not (yet) carried over from codiff

LLM walkthroughs, GitHub and GitLab pull request review, Markdown previews,
editing files in place, definition lookup, and `a..b` ranges.
