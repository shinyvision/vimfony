<?php
namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class Address {
    #[ORM\Column]
    private string $street;

    #[ORM\Column]
    private string $city;

    #[ORM\ManyToOne(targetEntity: User::class)]
    private User $user;

    /** Not a Doctrine field — should NOT appear in QB completions */
    private string $internalNote;
}
