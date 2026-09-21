<?php

namespace App\Traits;

/**
 * A calls members that only its consumers declare.
 */
trait A
{
    public function run(): void
    {
        $this->abc();
        $this->fromC();
    }
}
