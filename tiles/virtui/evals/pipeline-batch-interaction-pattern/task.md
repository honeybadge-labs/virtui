# Automate a Python REPL Session

## Problem Description

Your team is building a CI pipeline that validates a Python utility module by interactively running it inside a Python REPL session. The goal is to automate a sequence of steps: launch Python, import the module, call a function, print the result, and confirm the output is correct — all without any human at the keyboard.

The utility module is simple: it exposes a function `add(a, b)` that returns the sum of two numbers. You need to write a shell script (named `run_session.sh`) that uses virtui to drive this entire interaction. The script should launch a Python REPL, execute the sequence of REPL commands to import and test the module, capture the output, and clean up properly when done. After running, the script should write a summary of what it observed (the final screen output) to a file named `session_output.txt`.

## Output Specification

- `run_session.sh` — an executable shell script that drives the virtui session
- `session_output.txt` — a text file containing the captured screen output from the REPL session showing the result of the `add` call
- `module.py` — the Python file containing the `add(a, b)` function that is tested

The script should be self-contained: running `bash run_session.sh` from the same directory should execute the whole flow from daemon check through session cleanup.

## Input Files

The following files are provided as inputs. Extract them before beginning.

=============== FILE: inputs/module.py ===============
def add(a, b):
    return a + b

if __name__ == "__main__":
    print(add(1, 2))
