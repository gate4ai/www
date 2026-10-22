// deploy/deploy.sh: сбой docker compose pull не меняет .env (иначе после перезапуска хоста
// compose поднимал бы тег, которого нет), удачный выкат пишет тег и сохраняет прежний.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// deployEnv — каталог проекта с .env и PATH с фейковыми docker и curl: pull падает для тега
// «broken», health (curl) всегда отвечает.
func deployEnv(t *testing.T) (dir string, env []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("deploy.sh runs on the Linux server")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir = t.TempDir()
	bin := t.TempDir()
	write := func(path, body string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bin, "docker"), `#!/bin/sh
if [ "$1 $2" = "compose pull" ] && [ "${WWW_TAG:-}" = broken ]; then echo "pull: no such image" >&2; exit 1; fi
exit 0
`, 0o755)
	write(filepath.Join(bin, "curl"), "#!/bin/sh\nexit 0\n", 0o755)
	write(filepath.Join(dir, ".env"), "WWW_TAG=good\n", 0o644)
	env = append(os.Environ(), "GATE4AI_WWW_DIR="+dir, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, env
}

func runDeploy(t *testing.T, env []string, args ...string) error {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"deploy/deploy.sh"}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	t.Logf("deploy.sh %s: %s", strings.Join(args, " "), out)
	return err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDeployPullFailureKeepsEnv(t *testing.T) {
	dir, env := deployEnv(t)
	if err := runDeploy(t, env, "up", "broken"); err == nil {
		t.Fatal("deploy.sh up succeeded although the pull failed")
	}
	if got := readFile(t, filepath.Join(dir, ".env")); got != "WWW_TAG=good\n" {
		t.Fatalf(".env after a failed pull = %q, want the running tag", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env.prev")); err == nil {
		t.Fatal(".env.prev written although nothing was deployed")
	}
}

func TestDeployUpWritesTag(t *testing.T) {
	dir, env := deployEnv(t)
	if err := runDeploy(t, env, "up", "next"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, ".env")); got != "WWW_TAG=next\n" {
		t.Fatalf(".env = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, ".env.prev")); got != "WWW_TAG=good\n" {
		t.Fatalf(".env.prev = %q", got)
	}
}
