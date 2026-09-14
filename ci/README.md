# CI

`github-ci.yml` is the GitHub Actions workflow: it runs `gofmt`, `go vet`
and `go test ./...` on Ubuntu, Windows and macOS, cross-builds all 6
platform binaries, and runs the TypeScript SDK tests.

To enable it, copy it into place (this needs a token/user with the
`workflows` permission — the file cannot be pushed by restricted bots):

```bash
mkdir -p .github/workflows
cp ci/github-ci.yml .github/workflows/ci.yml
git add .github/workflows/ci.yml
git commit -m "Enable CI"
git push
```
