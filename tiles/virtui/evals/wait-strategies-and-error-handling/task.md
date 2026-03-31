# Monitor a Long-Running Build Process

## Problem Description

A build engineering team needs a script that monitors a slow shell command — one that takes a variable amount of time to complete — and reliably captures the final output even under timing uncertainty. The command prints progress dots to the terminal before completing, then shows a final "BUILD COMPLETE" status line. Sometimes the build finishes quickly, sometimes it takes up to 90 seconds.

The team has had problems with earlier automation scripts that either timed out too eagerly or polled the screen inefficiently by comparing entire screen dumps. You need to write a script (`monitor_build.sh`) that uses virtui to launch the build command, wait for it to finish intelligently, capture the final state, and write a build report. The script should handle the case where a wait times out gracefully: if the condition isn't met in time, it should take a screenshot to diagnose the current state and log the screen content to an error file before exiting.

The "build command" to simulate is: `bash -c 'for i in 1 2 3 4 5; do echo "Building step $i..."; sleep 2; done; echo "BUILD COMPLETE"'`

## Output Specification

- `monitor_build.sh` — the shell script that drives the virtui session
- `build_report.txt` — written on success, containing the captured screen output showing BUILD COMPLETE
- `error_state.txt` — written only if a timeout occurs, containing the screen content at time of timeout
- A brief `README.txt` explaining the wait strategy chosen and why
