FROM maven:3.9.11-eclipse-temurin-21-alpine@sha256:922927df2c662cdd47ddb116443d6bec4696cfae3de1a0ddac8fcc7b87ce61ae AS providers

WORKDIR /build/naver
COPY deployments/keycloak/providers/naver/pom.xml ./pom.xml
COPY deployments/keycloak/providers/naver/src ./src
RUN mvn --batch-mode --no-transfer-progress package -Dmaven.test.skip=true

ARG APPLE_PROVIDER_VERSION=1.17.0
ARG APPLE_PROVIDER_SHA256=4091dee2a1ec9e0771bef4bd46005197d86b0a2b1f25198c41738476b1d102bb
RUN wget -q -O /build/apple-identity-provider-1.17.0.jar \
      "https://github.com/klausbetz/apple-identity-provider-keycloak/releases/download/${APPLE_PROVIDER_VERSION}/apple-identity-provider-${APPLE_PROVIDER_VERSION}.jar" \
    && echo "${APPLE_PROVIDER_SHA256}  /build/apple-identity-provider-1.17.0.jar" | sha256sum -c -

FROM quay.io/keycloak/keycloak:26.7.1@sha256:f1f1f01e472c8a78df40d8f2a49a925274eda4d3d80d5f6edbb5c880ee3c01c6

COPY --from=providers /build/naver/target/naver-identity-provider-1.0.0.jar /opt/keycloak/providers/naver-identity-provider-1.0.0.jar
COPY --from=providers /build/apple-identity-provider-1.17.0.jar /opt/keycloak/providers/apple-identity-provider-1.17.0.jar
COPY deployments/keycloak/themes/patty /opt/keycloak/themes/patty
COPY deployments/keycloak/realm-patty.json /opt/keycloak/data/import/realm-patty.json

RUN /opt/keycloak/bin/kc.sh build --health-enabled=true

