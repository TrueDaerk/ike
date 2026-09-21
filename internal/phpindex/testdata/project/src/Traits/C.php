<?php

namespace App\Traits;

trait C
{
    protected $x;

    const K = 1;

    /**
     * From C.
     *
     * @return int|null
     */
    public function fromC(array $items = []): ?int
    {
        return null;
    }

    public static function make(): static
    {
        return new static();
    }
}
