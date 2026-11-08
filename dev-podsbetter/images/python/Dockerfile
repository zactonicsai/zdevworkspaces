# Python developer image. Build after the base:
#   docker build --build-arg BASE=REGISTRY/dev-pod-base:24.04 --build-arg PY=3.12 -t REGISTRY/dev-pod-python:3.12 images/python
ARG BASE=dev-pod-base:24.04
FROM ${BASE}
ARG PY=3.12

USER root
RUN apt-get update && apt-get install -y --no-install-recommends software-properties-common gnupg \
 && add-apt-repository -y ppa:deadsnakes/ppa \
 && apt-get update && apt-get install -y --no-install-recommends \
      python${PY} python${PY}-venv python${PY}-dev python3-pip pipx libffi-dev libssl-dev libpq-dev \
 && update-alternatives --install /usr/bin/python3 python3 /usr/bin/python${PY} 1 \
 && update-alternatives --install /usr/bin/python python /usr/bin/python${PY} 1 \
 && apt-get purge -y software-properties-common && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

# common tooling, installed for the dev user
USER dev
ENV PATH=/home/dev/.local/bin:$PATH PIP_BREAK_SYSTEM_PACKAGES=1
RUN python3 -m pip install --user --no-cache-dir --upgrade pip \
 && python3 -m pip install --user --no-cache-dir uv poetry pytest black ruff mypy ipython httpie
ENV DEV_POD_IMAGE=dev-pod-python:${PY}
