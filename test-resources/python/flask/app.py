# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

from flask import Flask, request, jsonify
import logging
import os


app = Flask(__name__)
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)
port = int(os.environ.get("PORT", 1507))


@app.route("/ready")
def ready():
    return ('', 204)


@app.route("/dash0-k8s-operator-test")
def test():
    req_id = request.args.get('id', default=None, type=str)
    if req_id:
        logger.info("processing request %s", req_id)
    else:
        logger.info("processing request")
    return jsonify(
        message = "We make Observability easy for every developer.",
    )


if __name__ == '__main__':
    app.run(host="0.0.0.0", port=port)

