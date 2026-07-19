// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import lucode from 'lucode-starlight';

// https://astro.build/config
export default defineConfig({
  // Update `site` + `base` when you wire up deployment (e.g. GitHub Pages).
  site: 'https://thapakazi.github.io',
  base: '/kazi',
  integrations: [
    starlight({
      title: 'kazi',
      tagline: 'The control plane for your local containers',
      logo: {
        src: './src/assets/logo.svg',
        replacesTitle: false,
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/thapakazi/kazi',
        },
      ],
      plugins: [lucode()],
      sidebar: [
        {
          label: 'Start here',
          items: [
            { label: 'Introduction', link: '/' },
            { label: 'Installation', link: '/getting-started/installation/' },
            { label: 'Usage', link: '/getting-started/usage/' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Commands', link: '/reference/commands/' },
            { label: 'For LLMs & agents', link: '/reference/llms/' },
          ],
        },
      ],
    }),
  ],
});
