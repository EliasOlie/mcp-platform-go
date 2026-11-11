import sys, subprocess, os

log_file = "/tmp/janus_raw_mcp.log"
with open(log_file, "a") as f:
    f.write("=== NOVA CONEXAO MCP ===\n")

proc = subprocess.Popen(["/home/eliasolie/work/janus/bin/janus", "--stdio"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

import threading
def pipe_in():
    with open(log_file, "a") as f:
        for line in sys.stdin:
            f.write(f"IN: {line}")
            f.flush()
            proc.stdin.write(line)
            proc.stdin.flush()

def pipe_out():
    with open(log_file, "a") as f:
        for line in proc.stdout:
            f.write(f"OUT: {line}")
            f.flush()
            sys.stdout.write(line)
            sys.stdout.flush()

t1 = threading.Thread(target=pipe_in, daemon=True)
t2 = threading.Thread(target=pipe_out, daemon=True)
t1.start()
t2.start()
proc.wait()
