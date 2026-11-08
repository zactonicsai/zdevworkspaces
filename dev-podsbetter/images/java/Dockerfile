# Java developer image. Build after the base:
#   docker build --build-arg BASE=REGISTRY/dev-pod-base:24.04 --build-arg JDK=21 -t REGISTRY/dev-pod-java:21 images/java
ARG BASE=dev-pod-base:24.04
FROM ${BASE}
ARG JDK=21
ARG MAVEN_VERSION=3.9.9
ARG GRADLE_VERSION=8.11.1

USER root
RUN apt-get update && apt-get install -y --no-install-recommends gnupg \
 && mkdir -p /etc/apt/keyrings \
 && curl -fsSL https://packages.adoptium.net/artifactory/api/gpg/key/public | gpg --dearmor -o /etc/apt/keyrings/adoptium.gpg \
 && echo "deb [signed-by=/etc/apt/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb noble main" > /etc/apt/sources.list.d/adoptium.list \
 && apt-get update && apt-get install -y --no-install-recommends temurin-${JDK}-jdk \
 && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL "https://dlcdn.apache.org/maven/maven-3/${MAVEN_VERSION}/binaries/apache-maven-${MAVEN_VERSION}-bin.tar.gz" | tar xz -C /opt \
 && ln -s /opt/apache-maven-${MAVEN_VERSION} /opt/maven \
 && curl -fsSL "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip" -o /tmp/gradle.zip \
 && unzip -q /tmp/gradle.zip -d /opt && ln -s /opt/gradle-${GRADLE_VERSION} /opt/gradle && rm /tmp/gradle.zip

ENV JAVA_HOME=/usr/lib/jvm/temurin-${JDK}-jdk-amd64 \
    MAVEN_HOME=/opt/maven GRADLE_HOME=/opt/gradle \
    PATH=/opt/maven/bin:/opt/gradle/bin:$PATH \
    DEV_POD_IMAGE=dev-pod-java:${JDK}
USER dev
