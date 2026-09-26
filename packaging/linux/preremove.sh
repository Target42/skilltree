#!/bin/sh
set -e
systemctl stop skilltree.service 2>/dev/null || true
systemctl disable skilltree.service 2>/dev/null || true
