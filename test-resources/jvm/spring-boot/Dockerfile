# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

ARG JDK_VERSION=25

FROM eclipse-temurin:${JDK_VERSION}-jdk-noble

RUN groupadd -r appuser && useradd -r -g appuser -m -s /bin/bash appuser

WORKDIR /app

COPY --chmod=755 mvnw ./
COPY --chmod=644 pom.xml ./
COPY --chmod=755 .mvn ./.mvn
COPY --chmod=755 src ./src

COPY --chmod=755 --chown=appuser:appuser .m2 /home/appuser/.m2
RUN chown -R appuser:appuser /app

USER appuser

RUN JAVA_HOME=$(which java | xargs dirname | xargs dirname) \
    ./mvnw -gs /home/appuser/.m2/settings.xml \
    -Dmaven.repo.local=/home/appuser/.m2/repository \
    package

ENTRYPOINT ["java", "-jar", "/app/target/app.jar"]
