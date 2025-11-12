FROM alpine:3.20
RUN apk add --no-cache bash iproute2 iptables
WORKDIR /chaos
COPY chaos.sh .
ENTRYPOINT ["bash","/chaos/chaos.sh"]
