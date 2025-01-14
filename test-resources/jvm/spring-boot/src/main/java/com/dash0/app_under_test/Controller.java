package com.dash0.app_under_test;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class Controller {

    @Autowired
    private ObjectMapper mapper;

    @GetMapping("/ready")
    @ResponseStatus(code = HttpStatus.NO_CONTENT)
    public void ready() {
    }

    @GetMapping(path="/dash0-k8s-operator-test", produces= MediaType.APPLICATION_JSON_VALUE)
    public ObjectNode test() {
        ObjectNode response = mapper.createObjectNode();
        response.put("message", "We make Observability easy for every developer.");
        return response;
    }
}