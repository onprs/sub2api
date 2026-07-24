#!/usr/bin/env python3
"""Static scan for GitHub Actions workflow command-injection vectors (H-4).

It flags any `${{ inputs.* }}`, `${{ github.event.inputs.* }}` or
`${{ steps.<id>.outputs.* }}` expression that is interpolated directly inside a
`run:` block. Those user-/step-controlled values must be passed through `env:`
and referenced as ordinary shell variables in `run:` instead, otherwise a
malicious tag/comment can escape the surrounding quotes and execute arbitrary
commands on the runner.

Exit codes:
  0  no findings
  1  dangerous expressions found inside `run:` blocks
  2  unable to parse one or more workflows (e.g. missing PyYAML)
"""
from __future__ import annotations

import glob
import re
import sys
from typing import Iterable

try:
    import yaml  # type: ignore
except ImportError:  # pragma: no cover
    print('PyYAML is required to parse workflows: python -m pip install pyyaml', file=sys.stderr)
    sys.exit(2)


_DANGEROUS = re.compile(
    r"""\$\{\{\s*(?:                  # ${{ ...
        inputs\.|                       #   inputs.<field>
        github\.event\.inputs\.|        #   github.event.inputs.<field>
        steps\.[A-Za-z_][\w-]*\.outputs\.  # steps.<id>.outputs.<field>
        )""",
    re.VERBOSE,
)


def _workflow_paths(argv: list[str]) -> list[str]:
    if argv:
        return argv
    return sorted(
        set(glob.glob('.github/workflows/*.yml')) | set(glob.glob('.github/workflows/*.yaml'))
    )


def _iter_run_strings(workflow: object) -> Iterable[tuple[str, str]]:
    if not isinstance(workflow, dict):
        return
    jobs = workflow.get('jobs') or {}
    if not isinstance(jobs, dict):
        return
    for job_name, job in jobs.items():
        if not isinstance(job, dict):
            continue
        steps = job.get('steps') or []
        if not isinstance(steps, list):
            continue
        for step in steps:
            if not isinstance(step, dict):
                continue
            run = step.get('run')
            if isinstance(run, str):
                yield str(job_name), run


def main(argv: list[str]) -> int:
    paths = _workflow_paths(argv)
    findings: list[tuple[str, str, int, str]] = []
    parse_errors: list[str] = []

    for path in paths:
        try:
            with open(path, encoding='utf-8') as fh:
                workflow = yaml.safe_load(fh)
        except FileNotFoundError:
            parse_errors.append(f'{path}: file not found')
            continue
        except Exception as exc:  # noqa: BLE001 - report any parse failure explicitly
            parse_errors.append(f'{path}: cannot parse YAML: {exc}')
            continue

        for job_name, run in _iter_run_strings(workflow):
            for match in _DANGEROUS.finditer(run):
                line_no = run[: match.start()].count('\n') + 1
                findings.append((path, job_name, line_no, match.group(0).strip()))

    if parse_errors:
        for msg in parse_errors:
            print(f'error: {msg}', file=sys.stderr)
        return 2

    if findings:
        print('Workflow injection findings (expression interpolated inside `run:`):')
        for path, job, line_no, expr in findings:
            print(f'  - {path}: job {job!r} run line {line_no}: {expr}')
        print()
        print(
            'Pass user-/step-controlled values via `env:` and reference them as '
            'shell variables (e.g. $TAG_INPUT) inside `run:` instead.'
        )
        return 1

    print(f'No `run:` expression injection found across {len(paths)} workflow file(s).')
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))