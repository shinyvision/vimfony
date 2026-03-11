<?php
namespace App\Repository;

use App\Entity\User;

class UserRepository
{
    public function findUsers()
    {
        $qb = $this->createQueryBuilder('u');
        $qb->join('u.address', 'a');
        $qb->where('u.id = 1');
        $qb->andWhere('a.');
    }

    public function findUsersWithFrom()
    {
        $qb = $this->createQueryBuilder('uu');
        $qb->from(User::class, 'uu');
        $qb->join('uu.address', 'aa');
        $qb->andWhere('aa.');
    }

    public function findUsersWithCollectionJoin()
    {
        $qb = $this->createQueryBuilder('u');
        $qb->leftJoin('u.addresses', 'addr');
        $qb->andWhere('addr.');
    }

    public function findUsersWithChannelJoin()
    {
        $qb = $this->createQueryBuilder('u');
        $qb->join('u.channels', 'c');
        $qb->andWhere('c.');
    }
}
