---
title: Author Bundles
description: Create a Bundle with Porter
aliases:
- /authoring-bundles/
---

Porter generates a bundle from its manifest, porter.yaml.
Manifests use [templates] for variable substitution.
The manifest is made up of multiple components:

* [Bundle Metadata](#bundle-metadata)
* [Mixins](#mixins)
* [Parameters](#parameters)
* [Outputs](#outputs)
* [Parameter and Output Schema](#parameter-and-output-schema)
* [Credentials](#credentials)
* [Bundle Actions](#bundle-actions)
* [Dependencies](#dependencies)
* [Images](#images)
* [Custom](#custom)
* [Required](#required)
* [Generated Files](#generated-files)

We have full [examples](https://porter.sh/src/examples) of Porter manifests in the Porter repository.

[templates]: /authors/templates/

## Bundle Metadata

A lot of the metadata is defined by the [CNAB Spec](https://github.com/cnabio/cnab-spec/blob/master/101-bundle-json.md)
although Porter does have extra fields that are specific to making Porter bundles.

```yaml
name: azure-wordpress
description: Install Wordpress on Azure
version: 0.1.0
registry: getporter
reference: getporter/azure-wordpress
dockerfile: dockerfile.tmpl
```

* `name`: The name of the bundle
* `description`: A description of the bundle
* `version`: The version of the bundle, uses [semver](https://semver.org). A leading v prefix may optionally be used.
* `registry`: The registry to use for publishing the bundle. The format is `REGISTRY_HOST/ORG`.
    Both the final bundle reference and invocation image name will be based on this value.
    For example, if the bundle name is `porter-hello`, registry is `getporter` and the version is `0.1.0`,
    the bundle reference will be `getporter/porter-hello:v0.1.0` and the invocation image name will be `getporter/porter-hello-installer:v0.1.0`
* `reference`: OPTIONAL. The bundle reference, taking precedence over any values set for the `registry`, `name` fields. The format is `REGISTRY_HOST/ORG/NAME`.  The recommended pattern is to let the Docker tag be auto-derived from the `version` field.  However, a full reference with a Docker tag included may also be specified.
   The invocation image name will also be based on this value when set. For example, if the `reference` is
   `getporter/porter-hello`, then the final invocation image name will be `getporter/porter-hello-installer:v0.1.0`. 
  
   When the version is used to default the tag, and it contains a plus sign (+), the plus sign is replaced with an underscore because while + is a valid semver delimiter for the build metadata, it is not an allowed character in a tag.
* `dockerfile`: OPTIONAL. The relative path to a Dockerfile to use as a template during `porter build`. 
    See [Custom Dockerfile](/custom-dockerfile/) for details on how to use a custom Dockerfile.
* `custom`: OPTIONAL. A map of [custom bundle metadata](https://github.com/cnabio/cnab-spec/blob/master/101-bundle-json.md#custom-extensions).

## Mixins

Mixins are adapters between the Porter and an existing tool or system. They know how to talk to Porter to include everything
they need to run, such as a CLI or config files, and how to execute their steps in the Porter manifest.

Anyone can [create a mixin](/mixin-dev-guide/), here's a list of the mixins that are installed with Porter by default:

* exec - run shell scripts and commands
* helm - use the helm cli
* azure - provision services on the Azure cloud

Declare the mixins that your bundle uses with the `mixins` section of the manifest:

```yaml
mixins:
- exec
```

Some mixins allow you to specify configuration data that is provided to the mixin during `porter build`. Each mixin
has its own format for the configuration. For example, the az mixin allows you to specify extensions to install
and the helm mixin takes repositories to configure:

```yaml
mixins:
- az:
    extensions:
    - azure-cli-iot-ext
- helm:
    repositories:
      bitnami:
        url: "https://charts.bitnami.com/bitnami"
```

See [Using Mixins](/use-mixins) to learn more about how mixins work.

## Parameters

Parameters are part of the [CNAB Spec](https://github.com/cnabio/cnab-spec/blob/master/101-bundle-json.md#parameters) and
allow you to pass in configuration values when you execute the bundle.

Learn more about [how parameters work in Porter](/parameters/).

```yaml
parameters:
- name: mysql_user
  default: azureuser
- name: mysql_password
  type: string
  sensitive: true
  applyTo:
    - install
    - upgrade
- name: database_name
  type: string
  default: "wordpress"
  env: MYSQL_DATABASE
- name: tfstate
  type: file
  path: /cnab/app/tfstate
  source:
    output: tfstate
- name: connection-string
  type: string
  source:
    dependency: mysql
    output: connstr
```

* `name`: The name of the parameter.
* `type`: The data type of the parameter: string, integer, number, boolean, or [file](#file-parameters). When omitted, 
  Porter will attempt to detect the type and default it to either file or string.
* `default`: (Optional) The default value for the parameter, which will be used if not supplied elsewhere.
* `env`: (Optional) The name for the destination environment variable in the bundle. Defaults to the name of the parameter in upper case, if path is not specified.
* `path`: (Optional) The destination file path in the bundle.
* `sensitive`: (Optional) Designate this parameter's value as sensitive, for masking in console output.
* `applyTo`: (Optional) Designate to which actions this parameter applies. When not supplied, it is assumed the parameter applies to all actions.
* `source`: (Optional) Define from where the parameter's value should be resolved. See [parameter sources](#parameter-sources) for an example.
  * `dependency`: (Optional) The name of the dependency that generated the output. If not set, the output must be
  generated by the current bundle.
  * `output`: An output name. The parameter's value is set to output's last value. If the output doesn't
  exist, then the parameter's default value is used when defined, otherwise the user is required to provide a value.

### File Parameters

Porter supports passing a file as a parameter to a bundle.

For instance, a bundle might declare a parameter mytar of type file, located at /root/mytar when the bundle is run:

```yaml
- name: mytar
  type: file
  path: /root/mytar

install:
  - exec:
      description: "Install"
      command: bash
      flags:
        c: tar zxvf /root/mytar
```

The syntax to pass a parameter to porter is the same for both regular and file parameters:

```console
$ porter install --param mytar=./my.tar.gz
```

### Parameter Sources

Parameters can also use the value from an output from the current bundle or one of its dependencies as its default value
using the `source` field when defining the parameter.

**Source an output from the current bundle**
```yaml
parameters:
- name: tfstate
  type: file
  path: /cnab/app/tfstate
  source:
    output: tfstate
```

**Source an output from a dependency**
```yaml
parameters:
- name: connection-string
  type: string
  source:
    dependency: mysql
    source: connstr
```

## Outputs

Outputs are part of the [CNAB Spec](https://github.com/cnabio/cnab-spec/blob/master/101-bundle-json.md#outputs) to
allow access to outputs generated during the course of executing a bundle.  These are global/bundle-wide outputs,
as opposed to step outputs described in [Parameters, Credentials and Outputs](/wiring/).

```yaml
outputs:
- name: mysql_user
  description: "MySQL user name"
- name: mysql_password
  type: string
  applyTo:
    - install
    - upgrade
- name: kubeconfig
  type: file
  path: /root/.kube/config
```

* `name`: The name of the output.
* `type`: The data type of the output: string, integer, number, boolean.  When omitted, Porter will attempt to detect 
  the type and default it to either file or string.
* `applyTo`: (Optional) Restrict this output to a given list of actions. If empty or missing, applies to all actions.
* `description`: (Optional) A brief description of the given output.
* `sensitive`: (Optional) Designate an output as sensitive. Defaults to false.
* `path`: (Optional) Path where the output file should be retrieved.

Outputs must either have the same name as an output from a step, meaning that the output is generated by a step, or
it must define a `path` where the output file can be located on the filesystem.

### Parameter and Output Schema

The [CNAB Spec for definitions](https://github.com/cnabio/cnab-spec/blob/master/101-bundle-json.md#definitions)
applies to both parameters and outputs.  Parameters and outputs can use [json schema 7](https://json-schema.org) 
properties to describe acceptable values. Porter uses a slightly [modified schema][json-schema] because CNAB disallows 
non-integer values.

Below are a few examples of common json schema properties and how they are used by Porter:

* `default`: The default value for the parameter. When a default is not provided, the parameter is required.
* `enum`: A list of allowed values.
* [Numeric Range](https://json-schema.org/understanding-json-schema/reference/numeric.html?highlight=minimum#range) 
    using `minimum`, `maximum`, `exclusiveMinimum` and `exclusiveMaximum`.
* String length using `minLength` and `maxLength`.

```yaml
parameters:
- name: color
  type: string
  default: blue
  enum:
  - red
  - green
  - blue
- name: size
  type: integer
  minimum: 1
  maximum: 11
- name: label
  type: string
  minLength: 3
```

[json-schema]: https://github.com/cnabio/cnab-spec/blob/master/schema/definitions.schema.json

## Credentials

Credentials are part of the [CNAB Spec](https://github.com/cnabio/cnab-spec/blob/master/802-credential-sets.md) and allow
you to pass in sensitive data when you execute the bundle, such as passwords or configuration files. 

Learn more about [how credentials work in Porter](/credentials/).

By default, all credential values are considered sensitive and will be masked in console output.

```yaml
credentials:
- name: password
  env: ROOT_PASSWORD
- name: username
  description: User to create on the cluster, defaults to root
  env: USERNAME
  required: false
- name: kubeconfig
  path: /root/.kube/config
  applyTo:
    - upgrade
    - uninstall
```

* `name`: The name of the credential on the local system.
* `description`: (OPTIONAL) A description of what the credential is and how it is used.
* `env`: The name of the environment variable to create with the value from the credential.
* `path`: The file path to create with the file from the credential.
* `required`: (OPTIONAL) Specifies if the credential must be provided for applicable actions. Defaults to true.
* `applyTo`: (Optional) Designate to which actions this credential applies. When not supplied, it is assumed the credential applies to all actions.

### See Also
* [porter credentials generate](/cli/porter_credentials_generate/)
* [How Credentials Work](/how-credentials-work/)
* [Wiring Credentials](/wiring/)

## Bundle Actions

Porter and its mixins supports the three CNAB actions: install, upgrade, and
uninstall. Within each action, you define an ordered list of steps to execute.
Each step has a mixin, a `description`, and optionally `outputs`.

```yaml
install:
- helm:
    description: "Install MySQL"
    name: mydb
    chart: bitnami/mysql
    version: 6.14.2
    set:
      db.name: "{{ bundle.parameters.database-name }}"
      db.user: "{{ bundle.parameters.mysql-user }}"
  outputs:
  - name: mysql-root-password
    secret: mydb-creds
    key: mysql-root-password
  - name: mysql-password
    secret: mydb-creds
    key: mysql-password
```

* `MIXIN`: The name of the mixin that will handle this step. In the example above, `helm` is the mixin.
* `description`: A description of the step, used for logging.
* `outputs`: Any outputs provided by the steps. The `name` is required but the rest of the the schema for the 
output is specific to the mixin. In the example above, the mixin will make the Kubernetes secret data available as outputs.
By default, all output values are considered sensitive and will be masked in console output.

### Custom Actions
You can also define custom actions, such as `status` or `dry-run`, and define steps for them just as you would for
the main actions (install/upgrade/uninstall). Most of the mixins support custom actions but not all do.

You have the option of declaring your custom action, though it is not required. Custom actions are defaulted 
to `stateless: false` and `modifies: true`. [Well-known actions][well-known-actions] defined in the CNAB specification 
are automatically defaulted, such as `dry-run`, `help`, `log`, and `status`. You do not need to declare custom 
actions unless you want to change the defaults.

You may want to declare your custom action when the action does not make any changes, and its execution should not 
be recorded (`stateless: true` and `modifies: false`). The `help` action is supported out-of-the-box by Porter
and is automatically defaulted to this definition so you do not need to declare it. If you have an action that is 
similar to `help`, but has a different name, you should declare it in the `customActions` section.

```
customActions:
  myhelp:
    description: "Print a special help message"
    stateless: true
    modifies: false
```

* `description`: Description of the action.
* `stateless`: Indicates that the action is purely informational, that credentials are not required, 
   and that Porter should not keep track of when this action was last executed.
* `modifies`: Indicates whether this action modifies resources managed by the bundle.

[well-known-actions]: https://github.com/cnabio/cnab-spec/blob/master/804-well-known-custom-actions.md

## Dependencies

Dependencies are an extension of the [CNAB Spec](https://github.com/cnabio/cnab-spec/blob/master/500-CNAB-dependencies.md).
See [dependencies](/dependencies/) for more details on how Porter handles dependencies.

```yaml
dependencies:
  - name: mysql
    reference: getporter/mysql:v0.1.0
    parameters:
      database_name: wordpress
      mysql_user: wordpress
```

* `name`: A short name for the dependent bundle that is used to reference the dependent bundle elsewhere in the bundle.
* `reference`: The reference where the bundle can be found in an OCI registry. The format should be `REGISTRY/NAME:TAG` where TAG is 
    the semantic version of the bundle.
* `parameters`: Optionally set default values for parameters in the bundle.

## Images

The `images` section of the Porter manifest corresponds to the [Image Map](https://github.com/cnabio/cnab-spec/blob/master/103-bundle-runtime.md#image-maps)
section of the CNAB Spec. These are images used in the bundle and declaring them enables Porter to manage the following for you:

* publishing the bundle [copies referenced images into the published bundle](/distribute-bundles/#image-references-after-publishing).
* [archiving the bundle](/archive-bundles/) includes the referenced images in the archive.

Here is an example:

```yaml
images:
  websvc:
      description: "A simple web service"
      imageType: "docker"
      repository: "jeremyrickard/devops-days-msp"
      digest: "sha256:85b1a9b4b60a4cf73a23517dad677e64edf467107fa7d58fce9c50e6a3e4c914"
```

This information is used to generate the corresponding section of the `bundle.json` and can be
used in [template expressions](/wiring), much like `parameters`, `credentials` and `outputs`, allowing you to build image references using 
the `repository` and `digest` attributes. For example:

```
image: "{{bundle.images.websvc.repository}}@{{bundle.images.websvc.digest}}"
```

At runtime, these will be updated appropriately if a bundle has been [copied](/copy-bundles). Note that while `tag` is available, you should prefer the use of `digest`.

Here is a breakdown of all the supported fields on an image in this section of the manifest:

* `description`: A description of the image.
* `imageType`: The type of image. Defaults to "docker". Allowed values: "oci", "docker".
* `repository`: The name of the image, of the form REGISTRY/ORG/IMAGE.
* `digest`: The repository digest of the image (not to be confused with the image id).
* `size`: The image size in bytes.
* `mediaType`: The media type of the image.
* `labels`: Key/value pairs used to specify identifying attributes of the image.
* `tag`: The tag of the image (only recommended when/if digest isn't known/available).

A last note on `digest`.  Taking the example of the library `nginx` Docker image, we can get the repository digest like so:

```console
 $ docker inspect nginx | jq -r '.[].RepoDigests'
[
  "nginx@sha256:a93c8a0b0974c967aebe868a186e5c205f4d3bcb5423a56559f2f9599074bbcd"
]
```

## Custom

The Custom section of a Porter manifest is intended for bundle authors to
capture custom data used by the bundle. It is a map of string keys to values of
any structure. Porter passes all custom data through to the resulting
`bundle.json` as-is, without attempting to parse or otherwise understand the
data.

```yaml
custom:
  some-custom-config:
    item: "value"
  more-custom-config:
    enabled: true
    succeed: "please!"
```

You can access custom data at runtime using the `bundle.custom.KEY.SUBKEY` templating.
For example, `{{ bundle.custom.more-custom-config.enabled}}` allows you to
access nested values from the custom section.

See the [Custom Extensions](https://github.com/cnabio/cnab-spec/blob/master/101-bundle-json.md#custom-extensions)
section of the CNAB Specification for more details.

## Required

The `required` section of a Porter manifest is intended for bundle authors to declare which
[Required Extensions](https://github.com/cnabio/cnab-spec/blob/master/101-bundle-json.md#required-extensions)
known and supported by Porter are needed to run the bundle.  Hence, all extension configuration data in this section
is processed by Porter at runtime; if unsupported extension configuration exists, Porter will error out accordingly.

Currently, Porter supports the following required extensions and configuration:

### Docker

Access to the host Docker daemon is necessary to run this bundle.

When the bundle is executed, this elevated privilege must be explicitly granted to the bundle using the
[Allow Docker Host Access configuration](/configuration/#allow-docker-host-access) setting.

**Name:** 

`docker`

**Configuration:**

  * `privileged: BOOLEAN` - OPTIONAL. Whether or not the `--privileged` flag should be set when the bundle's invocation image runs. Defaults to false.

Example:

```yaml
required:
  - docker:
      privileged: true
```

## Generated Files

In addition to the porter manifest, Porter generates a few files for you to create a compliant CNAB Spec bundle.

* `.cnab/`: This directory structure is created during `porter init`. You can add files to this directory and they will
be copied into the final bundle so that you can access them at runtime. The path to this directory at runtime is `/cnab`.
* `.cnab/app/run`: This file is created during `porter init` and should not be modified.
* `Dockerfile`: This file is generated during `porter build` and cannot be modified.

## See Also

* [Using Mixins](/use-mixins/)
* [Bundle Dependencies](/dependencies/)
* [Parameters, Credentials, Outputs, and Images in Porter](/wiring/)
