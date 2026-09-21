<?php

namespace Acme\Lib;

trait VendorTrait
{
    public function vendorHelper(): void
    {
    }
}

class VendorConsumer
{
    use VendorTrait;
}
