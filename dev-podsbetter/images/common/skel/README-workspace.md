# Your workspace

* `~/workspace` is where repositories are cloned.
* Everything under `/home/dev` lives on your volume and survives stop/start.
* Anything else (`sudo apt install`, `/opt`, `/tmp`) is rebuilt from the image on every start —
  ask for the tool to be added to the image instead.
