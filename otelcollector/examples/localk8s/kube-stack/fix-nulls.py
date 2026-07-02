#!/usr/bin/env python3
"""
Remove null array values and null valueFrom keys from rendered Helm manifests.
Required for opentelemetry-kube-stack 0.3.3 on clusters with strict admission webhooks.

Usage:
    helm template ... | python3 fix-nulls.py | kubectl apply -f -
    python3 fix-nulls.py rendered.yaml | kubectl apply -f -
"""

import sys
import re

def fix(text):
    # Replace null arrays with empty arrays
    text = re.sub(r':\s*\[null\]', ': []', text)
    text = re.sub(r':\s*null(\s*$)', r': []\1', text, flags=re.MULTILINE)
    # Drop lines with "valueFrom: null"
    lines = [l for l in text.splitlines() if not re.match(r'\s*valueFrom:\s*null\s*$', l)]
    return '\n'.join(lines)

if len(sys.argv) > 1:
    with open(sys.argv[1]) as f:
        data = f.read()
else:
    data = sys.stdin.read()

print(fix(data))
