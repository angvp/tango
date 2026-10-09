"""Drives a project's shell on a real pseudo-terminal.

Usage: shell_pty_driver.py <shell binary> <project dir> <home dir>

Each scenario starts the shell fresh, types as a person would and checks what
the terminal shows and how the process ends. Exits 0 when every scenario
passed; otherwise prints what went wrong and exits 1.
"""
import os
import pty
import re
import select
import signal
import sys
import termios
import time

binary, project, home = sys.argv[1:4]
CTRL_C, CTRL_D = b"\x03", b"\x04"
UP = b"\x1b[A"
cache = os.path.join(home, ".cache")


class Shell:
    def __init__(self, extra_env=None):
        env = {
            "PATH": os.environ["PATH"],
            "HOME": home,
            "XDG_CACHE_HOME": cache,
            "TERM": "xterm",
            "TANGO_DB_DSN": "sqlite://" + os.path.join(project, "app.db"),
        }
        env.update(extra_env or {})
        pid, fd = pty.fork()
        if pid == 0:
            os.chdir(project)
            os.execve(binary, [binary], env)
        self.pid, self.fd, self.seen = pid, fd, b""

    def read(self, timeout):
        ready, _, _ = select.select([self.fd], [], [], timeout)
        if not ready:
            return False
        try:
            data = os.read(self.fd, 4096)
        except OSError:
            return False
        if not data:
            return False
        self.seen += data
        return True

    def expect(self, pattern, timeout=15):
        """Waits for pattern (a regex over everything not yet consumed)."""
        deadline = time.time() + timeout
        regex = re.compile(pattern.encode() if isinstance(pattern, str) else pattern, re.S)
        while True:
            match = regex.search(self.seen)
            if match:
                self.seen = self.seen[match.end():]
                return match
            if time.time() > deadline or not self.read(max(0.0, deadline - time.time())):
                if time.time() > deadline:
                    fail("timed out waiting for %r; screen so far: %r" % (pattern, self.seen))

    def send(self, data):
        os.write(self.fd, data if isinstance(data, bytes) else data.encode())

    def line(self, text):
        self.send(text + "\r")

    def finish(self, timeout=15):
        """Waits for the process to end and returns its exit status."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            while self.read(0.05):
                pass
            done, status = os.waitpid(self.pid, os.WNOHANG)
            if done:
                return os.waitstatus_to_exitcode(status)
        os.kill(self.pid, signal.SIGKILL)
        fail("the shell did not exit; screen: %r" % self.seen)

    def echo_is_on(self):
        attrs = termios.tcgetattr(self.fd)
        return bool(attrs[3] & termios.ECHO) and bool(attrs[3] & termios.ICANON)


def fail(message):
    print("FAIL:", message)
    sys.exit(1)


def check(condition, message):
    if not condition:
        fail(message)


def scenario(name):
    print("scenario:", name, flush=True)


PROMPT = r"tango> "

scenario("prompt, evaluation and the startup pointer")
sh = Shell()
sh.expect(r"help\(\) lists the helpers")
sh.expect(PROMPT)
sh.line("1 + 1")
sh.expect(r"\r\n2\r\n")
sh.expect(PROMPT)
sh.line("n := 40")
sh.expect(PROMPT)
sh.line("n + 2")
sh.expect(r"\r\n42\r\n")
sh.expect(PROMPT)

scenario("a multi-line statement gets the continuation prompt")
sh.line("func double(x int) int {")
sh.expect(r"\.\.\.> ")
sh.line("return x * 2")
sh.expect(r"\.\.\.> ")
sh.line("}")
sh.expect(PROMPT)
sh.line("double(21)")
sh.expect(r"\r\n42\r\n")
sh.expect(PROMPT)

scenario("an error and a panic leave the session usable")
sh.line("undefinedName")
sh.expect(r"error: .*undefined")
sh.expect(PROMPT)
sh.line("for v := range func(yield func(int) bool) { yield(1) } { _ = v }")
sh.expect(r"panic: ")
sh.expect(PROMPT)
sh.line("n + 1")
sh.expect(r"\r\n41\r\n")
sh.expect(PROMPT)

scenario("a long value is cut at 200 characters with an ellipsis")
sh.line('import "strings"')
sh.expect(PROMPT)
sh.line('strings.Repeat("x", 300)')
shown = sh.expect(r"\r\n(\"x+…)\r\n").group(1).decode()
check(len(shown) == 201, "truncated value has %d characters, want 200 and an ellipsis" % len(shown))
sh.expect(PROMPT)

scenario("Ctrl-C clears the line; a line in between resets the count")
sh.send("half typed")
sh.send(CTRL_C)
sh.expect(r"\^C")
sh.expect(r"press Ctrl-C again")
sh.expect(PROMPT)
sh.line("3 + 4")
sh.expect(r"\r\n7\r\n")
sh.expect(PROMPT)
sh.send(CTRL_C)
sh.expect(r"press Ctrl-C again")
sh.expect(PROMPT)
sh.line("5")
sh.expect(r"\r\n5\r\n")
sh.expect(PROMPT)

scenario("a second Ctrl-C on an empty prompt exits cleanly and restores the terminal")
sh.send(CTRL_C)
sh.expect(PROMPT)
sh.send(CTRL_C)
status = sh.finish()
check(status == 0, "two Ctrl-C exit status = %d, want 0" % status)

scenario("Ctrl-D exits cleanly; the lines typed were saved")
sh = Shell()
sh.expect(PROMPT)
sh.line("first := 1")
sh.expect(PROMPT)
sh.line("first + 1")
sh.expect(r"\r\n2\r\n")
sh.expect(PROMPT)
sh.send(CTRL_D)
check(sh.finish() == 0, "Ctrl-D should exit 0")
files = [os.path.join(dp, f) for dp, _, fs in os.walk(home) for f in fs]
check(len(files) == 1, "expected one history file under the cache directory, found %r" % files)
saved = open(files[0]).read().splitlines()
check(saved[-2:] == ["first := 1", "first + 1"], "history file holds %r" % saved)
check(oct(os.stat(files[0]).st_mode & 0o777) == "0o600", "history file mode is %s" % oct(os.stat(files[0]).st_mode & 0o777))

scenario("history is available to the up arrow in the next session")
sh = Shell()
sh.expect(PROMPT)
sh.send(UP)
sh.expect(r"first \+ 1")
sh.send(CTRL_C)
sh.expect(PROMPT)
sh.send(CTRL_D)
check(sh.finish() == 0, "Ctrl-D should exit 0")

scenario("exit() ends the session with status 0")
sh = Shell()
sh.expect(PROMPT)
sh.line("exit()")
check(sh.finish() == 0, "exit() should exit 0")

scenario("Ctrl-C during an evaluation exits 130 with the terminal restored")
sh = Shell()
sh.expect(PROMPT)
sh.line("for { }")
time.sleep(0.5)
check(sh.echo_is_on(), "the terminal is still raw while an evaluation runs")
sh.send(CTRL_C)
status = sh.finish()
check(status == 130, "Ctrl-C during an evaluation exit status = %d, want 130" % status)

scenario("TANGO_SHELL_HISTORY=off saves nothing")
for dp, _, fs in os.walk(home):
    for f in fs:
        os.remove(os.path.join(dp, f))
sh = Shell({"TANGO_SHELL_HISTORY": "off"})
sh.expect(PROMPT)
sh.line("secret := 1")
sh.expect(PROMPT)
sh.send(CTRL_D)
check(sh.finish() == 0, "Ctrl-D should exit 0")
files = [f for _, _, fs in os.walk(home) for f in fs]
check(files == [], "history off still wrote %r" % files)

print("ok")
