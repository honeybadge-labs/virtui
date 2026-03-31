# Create a Terminal Demo Recording for Developer Onboarding

## Problem Description

Your team maintains a CLI tool called `data-cli` and wants to create a polished onboarding demo for new developers. The demo should show the complete first-time setup workflow: initializing a new project directory, creating a configuration file, and running a sample data transformation command. The recording will be embedded in internal documentation and played back using asciinema.

The demo needs to be saved to a specific path (`./demo/onboarding.cast`) so it can be committed into the docs repository. Write a script (`record_demo.sh`) that uses virtui to create the recording. The script should drive the session through the full workflow sequence, then finalize the recording. After the script finishes, the `.cast` file must exist at the specified path.

Since `data-cli` isn't actually installed, simulate the workflow using standard shell commands that represent the same logical steps: create a new directory called `myproject`, write a JSON config file into it, and echo a mock transformation result. The important thing is that the recording captures a realistic-looking multi-step terminal session.

## Output Specification

- `record_demo.sh` — an executable shell script that creates the terminal recording
- `demo/onboarding.cast` — the asciicast v2 recording file produced by running the script
