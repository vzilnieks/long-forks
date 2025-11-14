FROM alpine:3.20
RUN apk add --no-cache bash iproute2 iptables iputils-ping ca-certificates
# WORKDIR /chaos
# COPY chaos.sh .
# ENTRYPOINT ["bash","/chaos/chaos.sh"]

# FROM ubuntu:22.04
# RUN apt-get update && \
#     DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
#       iproute2 iputils-ping ca-certificates && \
#     rm -rf /var/lib/apt/lists/*

# Контейнер ничего не делает сам — оптимизатор будет дергать tc через docker exec
ENTRYPOINT ["sleep", "infinity"]
