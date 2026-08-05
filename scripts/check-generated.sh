#!/bin/sh
set -eu

make gen
git diff --exit-code
