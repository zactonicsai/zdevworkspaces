#!/usr/bin/env bash
# OpenJDK 21 (Ubuntu package) + Maven + Gradle. ~/.m2 and ~/.gradle persist on the volume.
set -Eeuo pipefail; . /opt/workspace/lib.sh
apt_install openjdk-21-jdk-headless maven
: "${GRADLE_VERSION:=$(curl -fsSL https://services.gradle.org/versions/current | jq -r .version)}"
if [ ! -x /opt/gradle/bin/gradle ]; then
  fetch "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip" /tmp/gradle.zip
  rm -rf /opt/gradle /opt/gradle-* && unzip -q /tmp/gradle.zip -d /opt && mv "/opt/gradle-${GRADLE_VERSION}" /opt/gradle && rm -f /tmp/gradle.zip
fi
ln -sf /opt/gradle/bin/gradle /usr/local/bin/gradle
profile_env java \
  "export JAVA_HOME=$(dirname "$(dirname "$(readlink -f "$(command -v javac)")")")" \
  'export MAVEN_OPTS="-Dmaven.repo.local=$HOME/.m2/repository" GRADLE_USER_HOME="$HOME/.gradle"'
