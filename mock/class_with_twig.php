<?php

declare(strict_types=1);

namespace App;

use Symfony\Bundle\FrameworkBundle\Controller\AbstractController;
use Twig\Environment;
use Twig\Environment as TwigEnv;

class ClassWithTwig extends AbstractController
{
    private Environment $templating;

    public function __construct(Environment $templating)
    {
        $this->templating = $templating;
    }

    public function show(Environment $environment, TwigEnv $aliasedEnvironment): void
    {
        $view = $this->render('template.html.twig', []);

        $propView = $this->templating->render('template.html.twig', []);

        $paramView = $environment->render('@MyBundle/example.html.twig', []);

        /** @var Environment $docblockEnvironment */
        $docblockEnvironment = $aliasedEnvironment;
        $docblockView = $docblockEnvironment->render('template.html.twig', []);
    }
}
