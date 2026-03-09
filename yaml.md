I need an engine that can run development workflows involving coding agents. 

Should be able to use CLI, like this:

<CLI> spec.yaml
<CLI> resume <id> (in simple variant, preserved to disk)

Workflow consist of steps. 

```yaml
amata:
    - <step1>
    - <step2>
```

shell step

```
id: <unique>
type: shell
command: <shell script or command>
args:
    - <arg1>
    - <arg2>
pwd: <path to execute>
response: 
    type: <string|number|schema>
    schema: <schema in json or yaml format>
then: <stop|$id|next>
```

or 

```yaml
commands:
    - <shell script/command>
    - <shell script/command>
```

codex step

```yaml
id:
type: codex
prompt:
model: 
reasoning:
response:
    schema: <schema in json or yaml format>
then:
```

Response schema

```yaml
  $id: https://example.com/address.schema.json
  $schema: https://json-schema.org/draft/2020-12/schema
  description: An address similar to http://microformats.org/wiki/h-card
  type: object
  properties:
        postOfficeBox:
            $comment: A type of the box
            type: string
```

Catching

```yaml
catch: <stop|$id|next>
```

or 

```yaml
exit: 
    code: <[0-9]{1,}|non-zero>
    then: <stop|$id|next>
```

Control flows, can be used in `then:`

```yaml

then:
    each: <action for every element in output>

then:
    each: 
        call:  <action for every element in output>
        wait: all|any
        order: <seq|parallel>
        then: <postprocess if wait>

then:
    switch:
        - when: <expression>
        then:
```

Expressions

1. default: written in js.

2. receive input with object:

```js
path: <array of executed steps objects>
```
every object contains:
```js
id:
stdout:
stderr:
```

Any block can be declared separately in file or outside:

```yaml
case:
    - when: #expressionX
    
expressionX:
    lang: js
    expr: |
        ...
        return X;
```

Can use docker (not hard criteria):

```yaml
docker:
  image:
```
