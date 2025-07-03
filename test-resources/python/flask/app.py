from flask import Flask, request, jsonify
import logging

app = Flask(__name__)
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


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

