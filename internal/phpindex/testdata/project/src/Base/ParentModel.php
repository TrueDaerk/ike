<?php

namespace App\Base;

abstract class ParentModel extends GrandParent
{
    protected int $id = 0;

    public function __construct(private readonly string $label = '')
    {
    }

    /** Find one by id. */
    public static function find(int $id): static
    {
        return new static();
    }
}

class GrandParent extends GreatGrandParent
{
    public function save(): bool
    {
        return true;
    }
}

class GreatGrandParent extends Ancestor
{
    public function touch(): void
    {
    }
}

class Ancestor
{
    public function beyond(): void
    {
    }
}
