# Git Synchronization & Conflict Resolution Guide

This runbook provides a foolproof, step-by-step procedure for synchronizing your local repository when **`main` has been updated on GitHub** (e.g., via a merged Pull Request) while you have **uncommitted local changes** or **diverged local commits**.

---

## 1. The Scenario & Why Direct `git pull` Fails

### What happened:
1. `origin/main` on GitHub advanced ahead with new commits (e.g., architecture refactors, dependency updates, PR merges).
2. Your local machine has active uncommitted work in the working directory (and/or commits not yet on remote).

### Why running `git pull` or `git merge` directly fails:
If your working tree is dirty (modified/untracked files), Git will abort with:
```text
error: Your local changes to the following files would be overwritten by merge.
Please commit your changes or stash them before you merge.
```
Forcing a pull or blindly running commands risks overwriting your local code or losing uncommitted work.

---

## 2. The Standard Safe 6-Step Sync Procedure (Recommended)

Follow these steps in order whenever remote `main` has moved ahead:

### Step 1: Fetch remote changes without touching your working tree
```bash
git fetch origin
```
This updates `origin/main` in your local Git tracking references without modifying any local files.

### Step 2: Create a safety backup branch
```bash
# Creates a pointer to your current state so you can NEVER lose anything
git branch backup-pre-sync-$(date +%Y%m%d)
```

### Step 3: Save your local work onto a dedicated feature branch
```bash
# 1. Switch to a new feature branch
git checkout -b feature/my-current-work

# 2. Stage and commit all uncommitted changes
git add -A
git commit -m "feat: work in progress before syncing with remote main"
```
*Your working directory is now 100% clean, and all your work is safely committed.*

### Step 4: Switch to `main` and align it with GitHub
```bash
# 1. Switch back to local main
git checkout main

# 2. Reset local main to exactly match GitHub main
git reset --hard origin/main
```
*Now local `main` is identical to GitHub `main` with zero drift.*

### Step 5: Merge your feature branch into `main`
```bash
git merge feature/my-current-work
```

- **Case A: No conflicts (Fast-Forward / Clean Merge)**:
  Git automatically merges all files. Skip to Step 6.

- **Case B: Merge Conflicts**:
  Git will report:
  ```text
  Auto-merging <file>
  CONFLICT (content): Merge conflict in <file>
  Automatic merge failed; fix conflicts and then commit the result.
  ```
  1. Open each conflicting file (look for `<<<<<<< HEAD`, `=======`, and `>>>>>>>`).
  2. Keep the desired logic from both sides and remove the `<<<<<<<`, `=======`, `>>>>>>>` marker lines.
  3. Verify no markers remain:
     ```bash
     git grep "<<<<<<<"
     ```
  4. Build and run tests to verify compilation and correctness:
     ```bash
     go build -o mycase main.go
     go test -v ./pkg/...
     ```
  5. Stage the resolved files and complete the merge commit:
     ```bash
     git add -A
     git commit -m "Merge branch 'feature/my-current-work' into main"
     ```

### Step 6: Push the integrated `main` back to GitHub
```bash
git push origin main
```

---

## 3. Alternative Quick Method: Using `git stash` (For Small Uncommitted Tweaks)

If you only have a couple of small uncommitted file tweaks and no local commits:

```bash
# 1. Stash uncommitted changes (including untracked files)
git stash --include-untracked

# 2. Pull remote main
git pull --rebase origin main

# 3. Re-apply your stashed changes
git stash pop

# 4. If there are stash conflicts, resolve them, test, and commit:
go test ./...
go build -o mycase main.go
```

> [!WARNING]
> If you have substantial refactors, newly created packages, or multi-commit divergence, **always use Method 2 (Feature Branch + Reset)** instead of `stash pop`. Method 2 preserves full commit history and gives you a safety branch to fall back on.

---

## 4. Emergency Recovery: What if something goes wrong?

Because you created a backup branch in Step 2, you can completely restore your exact previous state at any time:

```bash
# Restore main back to where it was before you started:
git checkout main
git reset --hard backup-pre-sync-<date>
```

---

## 5. Summary Cheat Sheet

| Goal | Command |
| :--- | :--- |
| Check status & incoming commits | `git fetch origin && git status` |
| See commits on remote not on local | `git log --oneline HEAD..origin/main` |
| See local commits not on remote | `git log --oneline origin/main..HEAD` |
| Safety branch | `git branch backup-$(date +%Y%m%d)` |
| Save local work to branch | `git checkout -b feature/work && git add -A && git commit -m "..."` |
| Sync local main to GitHub | `git checkout main && git reset --hard origin/main` |
| Merge feature into main | `git merge feature/work` |
| Search for leftover conflict markers | `git grep "<<<<<<<"` |
| Verify project compiles | `go build -o mycase main.go` |
| Push to GitHub | `git push origin main` |
