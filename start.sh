#!/bin/sh

cd $(dirname "$0")

llama-server --models-max 1 --models-preset preset.ini --host 0.0.0.0 --port 11434 --sleep-idle-seconds 180 -np 1
