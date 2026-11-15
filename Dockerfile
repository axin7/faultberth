FROM scratch
COPY faultberth /faultberth
USER 14237:14237
ENTRYPOINT ["/faultberth"]
