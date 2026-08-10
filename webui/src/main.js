import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import LoginView from './views/LoginView.vue'
import MachinesView from './views/MachinesView.vue'
import MachineDetailView from './views/MachineDetailView.vue'
import TrafficView from './views/TrafficView.vue'
import ActionsView from './views/ActionsView.vue'
import SettingsView from './views/SettingsView.vue'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginView },
    { path: '/', redirect: '/machines' },
    { path: '/machines', component: MachinesView },
    { path: '/machines/:id', component: MachineDetailView },
    { path: '/traffic', component: TrafficView },
    { path: '/actions', component: ActionsView },
    { path: '/settings', component: SettingsView },
  ],
})

createApp(App).use(router).mount('#app')
