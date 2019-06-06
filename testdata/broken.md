# mdurlcheck test

## Duplicate Subheading

* [Valid link](//example.com)
* [Valid link](#mdurlcheck-test)
* ![valid image](hello.md)
* [Valid link](./hello.md)
* [Valid link](broken.md#mdurlcheck-test)
* [Valid link to directory](../testdata)
* [Valid link to second duplicate subheading](#duplicate-subheading-1)

## Duplicate Subheading

* ![invalid image linking directory](../testdata)
* [Broken link](non-existent.md)
* [Broken link](#bam)
* [Broken link](broken.md#boom)
