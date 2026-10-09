"""Drives the admin password prompt on a real pseudo-terminal.

Usage: password_pty_driver.py <test binary>

The binary is the admin package's own test binary, re-run to execute only
TestReadPasswordHelperProcess, which reads one password with the prompt's
no-echo read. A small wrapper owns the terminal and runs the binary as its
foreground job, then reports how it ended and whether the terminal has its
echo back (the wrapper outlives the job, so the terminal can still be asked).

Two scenarios: a typed password is read without being shown, and Ctrl-C at the
prompt ends the process with status 130 and gives the terminal its echo back.
Exits 0 when both pass; otherwise prints what went wrong and exits 1.
"""
import os
import pty
import re
import select
import sys
import termios
import time

binary = sys.argv[1]

WRAPPER = r"""
import os, signal, subprocess, sys, termios
signal.signal(signal.SIGTTOU, signal.SIG_IGN)
job = subprocess.Popen([sys.argv[1], "-test.run=^TestReadPasswordHelperProcess$"],
                       env=dict(os.environ, ADMIN_PTY_HELPER="1"),
                       preexec_fn=lambda: os.setpgid(0, 0))
os.tcsetpgrp(0, job.pid)
code = job.wait()
echo = bool(termios.tcgetattr(0)[3] & termios.ECHO)
sys.stdout.write("RESULT code=%d echo=%d\n" % (code, echo))
sys.stdout.flush()
"""


def fail(message):
    print("FAIL:", message)
    sys.exit(1)


class Session:
    def __init__(self):
        pid, fd = pty.fork()
        if pid == 0:
            os.execv(sys.executable, [sys.executable, "-I", "-c", WRAPPER, binary])
        self.pid, self.fd, self.seen = pid, fd, b""

    def read(self, timeout):
        if not select.select([self.fd], [], [], timeout)[0]:
            return False
        try:
            data = os.read(self.fd, 4096)
        except OSError:
            return False
        self.seen += data
        return bool(data)

    def expect(self, pattern, timeout=15):
        deadline = time.time() + timeout
        regex = re.compile(pattern, re.S)
        while True:
            match = regex.search(self.seen)
            if match:
                self.seen = self.seen[match.end():]
                return match
            if time.time() > deadline:
                fail("timed out waiting for %r; saw %r" % (pattern, self.seen))
            self.read(0.05)

    def echo_is_on(self):
        return bool(termios.tcgetattr(self.fd)[3] & termios.ECHO)

    def wait_for_echo_off(self, timeout=15):
        """The read turns echo off once it waits for the password (its
        interrupt handler is installed by then), just after READY."""
        deadline = time.time() + timeout
        while self.echo_is_on():
            if time.time() > deadline:
                fail("echo stayed on while the password was being read")
            self.read(0.01)

    def send(self, data):
        os.write(self.fd, data)

    def result(self):
        match = self.expect(rb"RESULT code=(-?\d+) echo=(\d)\r?\n")
        os.waitpid(self.pid, 0)
        return int(match.group(1)), match.group(2) == b"1"


# 1. A typed password is read and never shown.
session = Session()
session.expect(rb"READY\r?\n")
session.wait_for_echo_off()
session.send(b"s3cret\r")
shown = session.expect(rb"READ:s3cret")
if b"s3cret" in shown.string[:shown.start()]:
    fail("the typed password was echoed: %r" % shown.string)
code, echo = session.result()
if code != 0 or not echo:
    fail("after a typed password: exit %d, echo restored: %s" % (code, echo))

# 2. Ctrl-C at the prompt restores echo and exits 130.
session = Session()
session.expect(rb"READY\r?\n")
session.wait_for_echo_off()
session.send(b"\x03")
code, echo = session.result()
if code != 130:
    fail("exit status after Ctrl-C = %d, want 130" % code)
if not echo:
    fail("the terminal was left without echo after Ctrl-C")
print("ok")
