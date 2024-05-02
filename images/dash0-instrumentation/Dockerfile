FROM alpine:3.19.1
RUN mkdir -p /dash0/instrumentation
COPY node.js /dash0/instrumentation/node.js
COPY copy-instrumentation.sh /
WORKDIR /
CMD ["/copy-instrumentation.sh"]
