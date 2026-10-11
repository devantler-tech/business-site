# Build context is ONLY runtime-dist/package.<id>, never the source checkout.
# Official keyless signature verified before adoption; includes system CA roots.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /app
COPY --chown=65532:65532 business-site build-receipt.json README.md /app/
COPY --chown=65532:65532 public/ /app/public/
COPY --chown=65532:65532 notices/ /app/notices/
USER 65532:65532
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/business-site"]
CMD ["--public", "/app/public", "--listen", "0.0.0.0:8080"]
