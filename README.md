[![build status](https://api.travis-ci.com/whitone/clickonce.svg?branch=master)](https://travis-ci.com/github/whitone/clickonce)
[![license](https://img.shields.io/github/license/whitone/clickonce)](./LICENSE)

# clickonce

> The `clickonce` package allows to download a [ClickOnce application].

## Example

Just import the library and set the URL of the ClickOnce application you want to download.

```go
import "github.com/whitone/clickonce"

co := &clickonce.ClickOnce{}
co.Init("https://my.cool.webserver/clickonce.application")
```

Then you can dump the remote contents related to requested application with the following method.

```go
err := co.GetAll()
if err != nil {
  log.Fatal(err)
}
```

And then, for example, you can check the content type and path of all deployed files.

```go
for dPath, dContent := range co.DeployedFiles() {
  fmt.Printf("%s: %s", dPath, http.DetectContentType(dContent))
}
```

If you want to save in a directory the application files, just set the output directory.
If that directory not exists, will be created.

```go
co.SetOutputDir("application")
```

If you need only some files from the application, just define your subset.

```go
err := co.Get([]string{"application.exe", "library.dll"})
if err != nil {
  log.Fatal(err)
}
```

To get more info about the progress of your request, you can enable verbose mode by passing your logger to the library.

```go
logger := log.New(os.Stdout, "", log.LstdFlags)
co.SetLogger(logger)
```

## Useful references

- [ClickOnce reference documentation]

## License

`clickonce` is licensed under the [BSD-3-Clause License](./LICENSE).

[ClickOnce application]: https://docs.microsoft.com/en-us/visualstudio/deployment/clickonce-security-and-deployment
[ClickOnce reference documentation]: https://docs.microsoft.com/en-us/visualstudio/deployment/clickonce-reference