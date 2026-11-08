.PHONY: run stub test build push
stub:   ; python3 api.py -stub -addr 127.0.0.1:8080
run:    ; python3 api.py -dev-user $${USER}@example.com -addr 127.0.0.1:8080
test:   ; python3 -W ignore -m unittest -v
build:  ; ./build.sh
push:   ; ./build.sh --push
