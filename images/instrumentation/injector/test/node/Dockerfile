ARG base_image
ARG instr_image

FROM ${instr_image} AS instr

FROM ${base_image}

# TODO Normalize node.js path

COPY ./test-cases /test-cases

COPY --from=instr /dash0-init-container /__dash0__
ENV LD_PRELOAD=/__dash0__/injector/dash0_injector.so